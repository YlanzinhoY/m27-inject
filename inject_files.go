package main

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	gameExecutableName   = "Madden27.exe"
	backupExecutableName = "Madden27_original.exe"
	fixedExecutableName  = "Madden27.fixed.exe"
)

func injectFile(gamePath string) error {
	info, err := os.Stat(gamePath)
	if err != nil {
		return fmt.Errorf("não foi possível acessar a pasta do Madden: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("o caminho do Madden não é uma pasta: %q", gamePath)
	}

	gameExecutable := filepath.Join(gamePath, gameExecutableName)
	backupExecutable := filepath.Join(gamePath, backupExecutableName)
	fixedExecutable := filepath.Join(gamePath, fixedExecutableName)

	if err := requireRegularFile(gameExecutable); err != nil {
		return fmt.Errorf("executável original inválido: %w", err)
	}
	if err := requireRegularFile(fixedExecutable); err != nil {
		return fmt.Errorf("executável corrigido inválido: %w", err)
	}
	if _, err := os.Lstat(backupExecutable); err == nil {
		return fmt.Errorf("o backup %q já existe; nenhuma alteração foi feita", backupExecutableName)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("não foi possível verificar o backup: %w", err)
	}

	if err := os.Rename(gameExecutable, backupExecutable); err != nil {
		return fmt.Errorf("não foi possível criar o backup de %q: %w", gameExecutableName, err)
	}
	if err := os.Rename(fixedExecutable, gameExecutable); err != nil {
		rollbackErr := os.Rename(backupExecutable, gameExecutable)
		if rollbackErr != nil {
			return fmt.Errorf(
				"não foi possível ativar %q: %w; também não foi possível restaurar o original: %v",
				fixedExecutableName,
				err,
				rollbackErr,
			)
		}
		return fmt.Errorf("não foi possível ativar %q; o original foi restaurado: %w", fixedExecutableName, err)
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
