package main

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/nwaples/rardecode/v2"
)

const downloadLink = "https://pixeldrain.com/api/file/AXCPM2gM"

type installationStage int

const (
	stageDownloading installationStage = iota
	stageExtracting
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
	return downloadAndExtract(context.Background(), http.DefaultClient, downloadLink, gamePath)
}

func downloadFilesWithProgress(gamePath string, report progressReporter) error {
	return downloadAndExtractWithProgress(
		context.Background(), http.DefaultClient, downloadLink, gamePath, report,
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
	request.Header.Set("User-Agent", "madden-27-inject/1.0")

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
	download := &downloadProgressWriter{
		writer:     archive,
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

	if err := extractRARWithProgress(archivePath, gamePath, report); err != nil {
		return fmt.Errorf("falha ao extrair os arquivos: %w", err)
	}
	return nil
}

func extractRAR(archivePath string, gamePath string) error {
	return extractRARWithProgress(archivePath, gamePath, nil)
}

func extractRARWithProgress(archivePath string, gamePath string, report progressReporter) error {
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
	reportProgress(report, installationProgress{
		Stage:      stageCopying,
		TotalFiles: fileCount,
	})
	return copyExtractedFilesWithProgress(stagingPath, gamePath, fileCount, report)
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
