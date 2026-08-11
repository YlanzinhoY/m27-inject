package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/nwaples/rardecode/v2"
)

const (
	downloadLink          = "https://pixeldrain.com/api/file/AXCPM2gM"
	expectedArchiveSHA256 = "0d3c93b5ce9f410ed8220fdb91081c55eaf752316d32403f23042cf02bb8aa67"
	preloaderDirectory    = "preloader_l.dll original"
	preloaderFileName     = "preloader_l.dll"
)

type installationStage int

const (
	stageDownloading installationStage = iota
	stageExtracting
	stageInjecting
	stageCopying
)

type installationProgress struct {
	Stage          installationStage
	Downloaded     int64
	TotalBytes     int64
	CompletedFiles int
	TotalFiles     int
}

type progressReporter func(installationProgress)

func downloadFiles(gamePath string) error {
	return downloadFilesWithProgress(gamePath, nil)
}

func downloadFilesWithProgress(gamePath string, report progressReporter) error {
	return downloadAndInstallVerified(
		context.Background(),
		http.DefaultClient,
		downloadLink,
		gamePath,
		expectedArchiveSHA256,
		report,
	)
}

func downloadAndExtract(ctx context.Context, client *http.Client, sourceURL string, gamePath string) error {
	return downloadAndExtractWithProgress(ctx, client, sourceURL, gamePath, nil)
}

func downloadAndExtractWithProgress(
	ctx context.Context,
	client *http.Client,
	sourceURL string,
	gamePath string,
	report progressReporter,
) error {
	return downloadAndExtractVerified(ctx, client, sourceURL, gamePath, "", report)
}

func downloadAndExtractVerified(
	ctx context.Context,
	client *http.Client,
	sourceURL string,
	gamePath string,
	expectedSHA256 string,
	report progressReporter,
) error {
	return downloadAndProcessArchive(ctx, client, sourceURL, gamePath, expectedSHA256, false, report)
}

func downloadAndInstallVerified(
	ctx context.Context,
	client *http.Client,
	sourceURL string,
	gamePath string,
	expectedSHA256 string,
	report progressReporter,
) error {
	return downloadAndProcessArchive(ctx, client, sourceURL, gamePath, expectedSHA256, true, report)
}

func downloadAndProcessArchive(
	ctx context.Context,
	client *http.Client,
	sourceURL string,
	gamePath string,
	expectedSHA256 string,
	installExecutable bool,
	report progressReporter,
) error {
	info, err := os.Stat(gamePath)
	if err != nil {
		return fmt.Errorf("não foi possível acessar a pasta do Madden: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("o caminho do Madden não é uma pasta: %q", gamePath)
	}

	archive, err := os.CreateTemp("", "madden-27-*.rar")
	if err != nil {
		return fmt.Errorf("não foi possível criar o arquivo temporário: %w", err)
	}
	archivePath := archive.Name()
	defer os.Remove(archivePath)

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		archive.Close()
		return fmt.Errorf("não foi possível preparar o download: %w", err)
	}
	request.Header.Set("User-Agent", "m27-inject/1.0")

	response, err := client.Do(request)
	if err != nil {
		archive.Close()
		return fmt.Errorf("falha ao baixar os arquivos: %w", err)
	}

	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		archive.Close()
		return fmt.Errorf("falha ao baixar os arquivos: servidor respondeu %s", response.Status)
	}

	reportProgress(report, installationProgress{
		Stage:      stageDownloading,
		TotalBytes: response.ContentLength,
	})
	archiveHash := sha256.New()
	download := &downloadProgressWriter{
		writer:     io.MultiWriter(archive, archiveHash),
		totalBytes: response.ContentLength,
		report:     report,
		lastReport: time.Now(),
	}
	written, copyErr := io.Copy(download, response.Body)
	download.finish()
	responseCloseErr := response.Body.Close()
	closeErr := archive.Close()
	if copyErr != nil {
		return fmt.Errorf("falha ao salvar o download: %w", copyErr)
	}
	if responseCloseErr != nil {
		return fmt.Errorf("falha ao finalizar o download: %w", responseCloseErr)
	}
	if closeErr != nil {
		return fmt.Errorf("falha ao finalizar o download: %w", closeErr)
	}
	if written == 0 {
		return fmt.Errorf("o servidor retornou um arquivo vazio")
	}
	actualSHA256 := fmt.Sprintf("%x", archiveHash.Sum(nil))
	if expectedSHA256 != "" && !strings.EqualFold(actualSHA256, expectedSHA256) {
		return fmt.Errorf(
			"checksum SHA-256 inválido: recebido %s, esperado %s",
			actualSHA256,
			expectedSHA256,
		)
	}

	if err := extractRARWithOptions(
		archivePath,
		gamePath,
		expectedSHA256 != "",
		installExecutable,
		report,
	); err != nil {
		return fmt.Errorf("falha ao extrair os arquivos: %w", err)
	}
	return nil
}

func extractRAR(archivePath string, gamePath string) error {
	return extractRARWithProgress(archivePath, gamePath, nil)
}

func extractRARWithProgress(archivePath string, gamePath string, report progressReporter) error {
	return extractRARWithOptions(archivePath, gamePath, false, false, report)
}

func extractRARWithOptions(
	archivePath string,
	gamePath string,
	verifiedArchive bool,
	installExecutable bool,
	report progressReporter,
) error {
	if verifiedArchive {
		return extractRARWithNativeTar(archivePath, gamePath, installExecutable, report)
	}
	return extractRARWithDecoder(archivePath, gamePath, report)
}

func extractRARWithDecoder(archivePath string, gamePath string, report progressReporter) error {
	reportProgress(report, installationProgress{Stage: stageExtracting})

	reader, err := rardecode.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer reader.Close()

	stagingPath, err := os.MkdirTemp("", "madden-27-extract-*")
	if err != nil {
		return fmt.Errorf("não foi possível criar a pasta temporária: %w", err)
	}
	defer os.RemoveAll(stagingPath)

	fileCount := 0
	for {
		header, nextErr := reader.Next()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			return nextErr
		}

		target, targetErr := archiveDestination(stagingPath, header.Name)
		if targetErr != nil {
			return targetErr
		}
		if header.IsDir {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if header.Mode().Type() != 0 {
			return fmt.Errorf("tipo de arquivo não permitido no RAR: %q", header.Name)
		}

		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		mode := header.Mode().Perm()
		if mode == 0 {
			mode = 0o644
		}
		output, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(output, reader)
		closeErr := output.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if !header.ModificationTime.IsZero() {
			_ = os.Chtimes(target, header.ModificationTime, header.ModificationTime)
		}
		fileCount++
		reportProgress(report, installationProgress{
			Stage:          stageExtracting,
			CompletedFiles: fileCount,
		})
	}

	if fileCount == 0 {
		return fmt.Errorf("o arquivo RAR não contém arquivos")
	}
	if err := movePreloaderToRoot(stagingPath); err != nil {
		return fmt.Errorf("não foi possível preparar %q: %w", preloaderFileName, err)
	}
	reportProgress(report, installationProgress{
		Stage:      stageCopying,
		TotalFiles: fileCount,
	})
	return copyExtractedFilesWithProgress(stagingPath, gamePath, fileCount, report)
}

func extractRARWithNativeTar(
	archivePath string,
	gamePath string,
	installExecutable bool,
	report progressReporter,
) error {
	reportProgress(report, installationProgress{Stage: stageExtracting})

	tarPath, err := exec.LookPath("tar.exe")
	if err != nil {
		return fmt.Errorf("o extrator nativo do Windows (tar.exe) não foi encontrado: %w", err)
	}

	stagingPath, err := os.MkdirTemp("", "madden-27-extract-*")
	if err != nil {
		return fmt.Errorf("não foi possível criar a pasta temporária: %w", err)
	}
	defer os.RemoveAll(stagingPath)

	listOutput, err := exec.Command(tarPath, "-tf", archivePath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("não foi possível listar o RAR: %w: %s", err, strings.TrimSpace(string(listOutput)))
	}
	entryCount := 0
	for _, entryName := range strings.Split(strings.ReplaceAll(string(listOutput), "\r\n", "\n"), "\n") {
		entryName = strings.TrimSuffix(entryName, "\r")
		if entryName == "" {
			continue
		}
		if _, err := archiveDestination(stagingPath, entryName); err != nil {
			return err
		}
		entryCount++
	}
	if entryCount == 0 {
		return fmt.Errorf("o arquivo RAR não contém arquivos")
	}

	extractOutput, err := exec.Command(tarPath, "-xf", archivePath, "-C", stagingPath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("não foi possível extrair o RAR: %w: %s", err, strings.TrimSpace(string(extractOutput)))
	}
	if err := movePreloaderToRoot(stagingPath); err != nil {
		return fmt.Errorf("não foi possível preparar %q: %w", preloaderFileName, err)
	}

	fileCount := 0
	err = filepath.WalkDir(stagingPath, func(_ string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("tipo de arquivo não permitido no RAR: %q", entry.Name())
		}
		fileCount++
		reportProgress(report, installationProgress{
			Stage:          stageExtracting,
			CompletedFiles: fileCount,
		})
		return nil
	})
	if err != nil {
		return err
	}
	if fileCount == 0 {
		return fmt.Errorf("o arquivo RAR não contém arquivos")
	}

	if installExecutable {
		return installExtractedFiles(stagingPath, gamePath, fileCount, report)
	}

	reportProgress(report, installationProgress{Stage: stageCopying, TotalFiles: fileCount})
	return copyExtractedFilesWithProgress(stagingPath, gamePath, fileCount, report)
}

func movePreloaderToRoot(stagingPath string) error {
	sourceDirectory := filepath.Join(stagingPath, preloaderDirectory)
	sourcePath := filepath.Join(sourceDirectory, preloaderFileName)
	destinationPath := filepath.Join(stagingPath, preloaderFileName)

	if err := requireRegularFile(sourcePath); err != nil {
		return fmt.Errorf("DLL obrigatória ausente na pasta %q: %w", preloaderDirectory, err)
	}
	if err := requireMissingPath(destinationPath); err != nil {
		return fmt.Errorf("destino da DLL inválido: %w", err)
	}
	if err := os.Rename(sourcePath, destinationPath); err != nil {
		return fmt.Errorf("não foi possível mover a DLL para a raiz: %w", err)
	}
	if err := os.Remove(sourceDirectory); err != nil {
		rollbackErr := os.Rename(destinationPath, sourcePath)
		if rollbackErr != nil {
			return fmt.Errorf(
				"não foi possível remover a pasta de origem: %w; também não foi possível devolver a DLL: %v",
				err,
				rollbackErr,
			)
		}
		return fmt.Errorf("não foi possível remover a pasta de origem; a DLL foi devolvida: %w", err)
	}
	return nil
}

type downloadProgressWriter struct {
	writer     io.Writer
	totalBytes int64
	written    int64
	lastReport time.Time
	report     progressReporter
}

func (writer *downloadProgressWriter) Write(data []byte) (int, error) {
	written, err := writer.writer.Write(data)
	writer.written += int64(written)

	now := time.Now()
	if now.Sub(writer.lastReport) >= 100*time.Millisecond ||
		(writer.totalBytes > 0 && writer.written >= writer.totalBytes) {
		writer.sendProgress()
		writer.lastReport = now
	}
	return written, err
}

func (writer *downloadProgressWriter) finish() {
	writer.sendProgress()
}

func (writer *downloadProgressWriter) sendProgress() {
	reportProgress(writer.report, installationProgress{
		Stage:      stageDownloading,
		Downloaded: writer.written,
		TotalBytes: writer.totalBytes,
	})
}

func reportProgress(report progressReporter, progress installationProgress) {
	if report != nil {
		report(progress)
	}
}

func archiveDestination(root string, archiveName string) (string, error) {
	normalized := strings.ReplaceAll(archiveName, `\`, "/")
	cleanName := path.Clean(normalized)
	localName := filepath.FromSlash(cleanName)
	if cleanName == "." || cleanName == ".." || strings.HasPrefix(cleanName, "../") ||
		path.IsAbs(cleanName) || filepath.IsAbs(localName) || filepath.VolumeName(localName) != "" {
		return "", fmt.Errorf("caminho inválido dentro do RAR: %q", archiveName)
	}

	target := filepath.Join(root, localName)
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("caminho inválido dentro do RAR: %q", archiveName)
	}
	return target, nil
}

func copyExtractedFiles(sourceRoot string, destinationRoot string) error {
	return copyExtractedFilesWithProgress(sourceRoot, destinationRoot, 0, nil)
}

func copyExtractedFilesWithProgress(
	sourceRoot string,
	destinationRoot string,
	totalFiles int,
	report progressReporter,
) error {
	copiedFiles := 0
	return filepath.WalkDir(sourceRoot, func(sourcePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		relative, err := filepath.Rel(sourceRoot, sourcePath)
		if err != nil || relative == "." {
			return err
		}
		destinationPath := filepath.Join(destinationRoot, relative)

		if entry.IsDir() {
			return os.MkdirAll(destinationPath, 0o755)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("tipo de arquivo não permitido: %q", relative)
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}
		input, err := os.Open(sourcePath)
		if err != nil {
			return err
		}
		output, err := os.OpenFile(destinationPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
		if err != nil {
			input.Close()
			return err
		}

		_, copyErr := io.Copy(output, input)
		inputCloseErr := input.Close()
		outputCloseErr := output.Close()
		if copyErr != nil {
			return copyErr
		}
		if inputCloseErr != nil {
			return inputCloseErr
		}
		if outputCloseErr != nil {
			return outputCloseErr
		}
		if err := os.Chtimes(destinationPath, info.ModTime(), info.ModTime()); err != nil {
			return err
		}
		copiedFiles++
		reportProgress(report, installationProgress{
			Stage:          stageCopying,
			CompletedFiles: copiedFiles,
			TotalFiles:     totalFiles,
		})
		return nil
	})
}
