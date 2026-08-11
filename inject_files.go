package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	gameExecutableName   = "Madden27.exe"
	backupExecutableName = "Madden27_original.exe"
	fixedExecutableName  = "Madden27.fixed.exe"
)

var errCrackAlreadyImplemented = errors.New("crack já foi implementado")

type executableInjection struct {
	gameExecutable   string
	backupExecutable string
}

func crackAlreadyImplemented(gamePath string) (bool, error) {
	for _, executableName := range []string{gameExecutableName, backupExecutableName} {
		executablePath := filepath.Join(gamePath, executableName)
		info, err := os.Lstat(executablePath)
		if os.IsNotExist(err) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("não foi possível verificar %q: %w", executableName, err)
		}
		if !info.Mode().IsRegular() {
			return false, fmt.Errorf("%q existe, mas não é um arquivo comum", executableName)
		}
	}
	return true, nil
}

func installExtractedFiles(
	stagingPath string,
	gamePath string,
	totalFiles int,
	report progressReporter,
) error {
	reportProgress(report, installationProgress{Stage: stageInjecting})
	injection, err := prepareExecutableInjection(gamePath, stagingPath)
	if err != nil {
		return fmt.Errorf("falha ao preparar o executável corrigido: %w", err)
	}

	reportProgress(report, installationProgress{Stage: stageCopying, TotalFiles: totalFiles})
	if err := copyExtractedFilesWithProgress(stagingPath, gamePath, totalFiles, report); err != nil {
		rollbackErr := injection.rollback()
		if rollbackErr != nil {
			return fmt.Errorf(
				"falha ao copiar os arquivos: %w; também não foi possível restaurar o executável original: %v",
				err,
				rollbackErr,
			)
		}
		return fmt.Errorf("falha ao copiar os arquivos; o executável original foi restaurado: %w", err)
	}
	return nil
}

func prepareExecutableInjection(gamePath string, stagingPath string) (*executableInjection, error) {
	if err := requireDirectory(gamePath); err != nil {
		return nil, fmt.Errorf("pasta do Madden inválida: %w", err)
	}
	if err := requireDirectory(stagingPath); err != nil {
		return nil, fmt.Errorf("pasta temporária inválida: %w", err)
	}

	gameExecutable := filepath.Join(gamePath, gameExecutableName)
	backupExecutable := filepath.Join(gamePath, backupExecutableName)
	stagedFixedExecutable := filepath.Join(stagingPath, fixedExecutableName)
	stagedGameExecutable := filepath.Join(stagingPath, gameExecutableName)

	if err := requireRegularFile(gameExecutable); err != nil {
		return nil, fmt.Errorf("executável original inválido: %w", err)
	}
	if err := requireRegularFile(stagedFixedExecutable); err != nil {
		return nil, fmt.Errorf("executável corrigido inválido: %w", err)
	}
	if err := requireMissingPath(backupExecutable); err != nil {
		return nil, fmt.Errorf("backup inválido: %w", err)
	}
	if err := requireMissingPath(stagedGameExecutable); err != nil {
		return nil, fmt.Errorf("o RAR já contém um %q inesperado: %w", gameExecutableName, err)
	}

	if err := os.Rename(gameExecutable, backupExecutable); err != nil {
		return nil, fmt.Errorf("não foi possível criar o backup de %q: %w", gameExecutableName, err)
	}
	if err := os.Rename(stagedFixedExecutable, stagedGameExecutable); err != nil {
		rollbackErr := os.Rename(backupExecutable, gameExecutable)
		if rollbackErr != nil {
			return nil, fmt.Errorf(
				"não foi possível preparar %q: %w; também não foi possível restaurar o original: %v",
				fixedExecutableName,
				err,
				rollbackErr,
			)
		}
		return nil, fmt.Errorf("não foi possível preparar %q; o original foi restaurado: %w", fixedExecutableName, err)
	}

	return &executableInjection{
		gameExecutable:   gameExecutable,
		backupExecutable: backupExecutable,
	}, nil
}

func (injection *executableInjection) rollback() error {
	if err := os.Remove(injection.gameExecutable); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("não foi possível remover o executável parcial: %w", err)
	}
	if err := os.Rename(injection.backupExecutable, injection.gameExecutable); err != nil {
		return err
	}
	return nil
}

func requireDirectory(directoryPath string) error {
	info, err := os.Stat(directoryPath)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%q não é uma pasta", directoryPath)
	}
	return nil
}

func requireMissingPath(filePath string) error {
	if _, err := os.Lstat(filePath); err == nil {
		return fmt.Errorf("%q já existe", filepath.Base(filePath))
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}

func requireRegularFile(filePath string) error {
	info, err := os.Lstat(filePath)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%q não é um arquivo comum", filepath.Base(filePath))
	}
	return nil
}
