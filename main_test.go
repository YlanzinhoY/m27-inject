package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestFindSteamGameFolderInAdditionalLibrary(t *testing.T) {
	installRoot := t.TempDir()
	libraryRoot := filepath.Join(t.TempDir(), "Steam Library")
	gamePath := filepath.Join(libraryRoot, "steamapps", "common", gameFolderName)
	if err := os.MkdirAll(gamePath, 0o755); err != nil {
		t.Fatal(err)
	}

	steamApps := filepath.Join(installRoot, "steamapps")
	if err := os.MkdirAll(steamApps, 0o755); err != nil {
		t.Fatal(err)
	}
	escapedLibraryPath := strings.ReplaceAll(libraryRoot, `\`, `\\`)
	vdf := `"libraryfolders"` + "\n{\n" +
		`  "1" { "path" "` + escapedLibraryPath + `" }` + "\n}\n"
	if err := os.WriteFile(filepath.Join(steamApps, "libraryfolders.vdf"), []byte(vdf), 0o600); err != nil {
		t.Fatal(err)
	}

	paths := findSteamGameFoldersInRoots(gameFolderName, []string{installRoot})
	if len(paths) != 1 || paths[0] != filepath.Clean(gamePath) {
		t.Fatalf("paths = %q, want %q", paths, gamePath)
	}
}

func TestValidateGamePath(t *testing.T) {
	gamePath := filepath.Join(t.TempDir(), gameFolderName)
	if err := os.Mkdir(gamePath, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := validateGamePath(`"` + gamePath + `"`)
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Clean(gamePath) {
		t.Fatalf("validateGamePath() = %q, want %q", got, gamePath)
	}
}

func TestInterfaceOpensManualPathScreen(t *testing.T) {
	model := newAppModel()

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(appModel)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(appModel)

	if model.screen != manualPathScreen {
		t.Fatalf("screen = %v, want manualPathScreen", model.screen)
	}
	if !strings.Contains(model.View(), "CAMINHO DO JOGO") {
		t.Fatalf("manual path view was not rendered: %q", model.View())
	}
}

func TestInterfaceAcceptsValidManualPath(t *testing.T) {
	gamePath := filepath.Join(t.TempDir(), gameFolderName)
	if err := os.Mkdir(gamePath, 0o755); err != nil {
		t.Fatal(err)
	}

	model := newAppModel()
	model.screen = manualPathScreen
	model.pathInput = gamePath

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(appModel)

	if model.screen != installScreen {
		t.Fatalf("screen = %v, want installScreen; error = %q", model.screen, model.errorMessage)
	}
	if model.selectedPath != filepath.Clean(gamePath) {
		t.Fatalf("selectedPath = %q, want %q", model.selectedPath, gamePath)
	}
	if command == nil {
		t.Fatal("expected installation command")
	}
}

func TestInterfaceShowsValidationError(t *testing.T) {
	model := newAppModel()
	model.screen = manualPathScreen
	model.pathInput = filepath.Join(t.TempDir(), "Outro Jogo")

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(appModel)

	if model.screen != errorScreen {
		t.Fatalf("screen = %v, want errorScreen", model.screen)
	}
	if model.errorMessage == "" {
		t.Fatal("expected validation error message")
	}
}

func TestArchiveDestinationRejectsPathTraversal(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"../outside.dll", `..\outside.dll`, "/outside.dll", `C:\outside.dll`} {
		if _, err := archiveDestination(root, name); err == nil {
			t.Errorf("archiveDestination(%q) accepted an unsafe path", name)
		}
	}
}

func TestArchiveDestinationAcceptsNestedFile(t *testing.T) {
	root := t.TempDir()
	got, err := archiveDestination(root, "mods/data/file.bin")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "mods", "data", "file.bin")
	if got != want {
		t.Fatalf("archiveDestination() = %q, want %q", got, want)
	}
}

func TestExtractRARCopiesFilesIntoDestination(t *testing.T) {
	destination := t.TempDir()
	archivePath := filepath.Join("testdata", "basic.rar")

	if err := extractRAR(archivePath, destination); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(destination, "link.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if info.IsDir() {
		t.Fatal("extracted link.txt is a directory")
	}
}

func TestDownloadAndExtractCopiesRARIntoDestination(t *testing.T) {
	archive, err := os.ReadFile(filepath.Join("testdata", "basic.rar"))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/vnd.rar")
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write(archive)
	}))
	defer server.Close()

	destination := t.TempDir()
	var progressEvents []installationProgress
	checksum := sha256.Sum256(archive)
	if err := downloadAndExtractVerified(
		context.Background(),
		server.Client(),
		server.URL,
		destination,
		fmt.Sprintf("%x", checksum[:]),
		func(progress installationProgress) {
			progressEvents = append(progressEvents, progress)
		},
	); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(destination, "link.txt")); err != nil {
		t.Fatal(err)
	}

	foundDownload := false
	foundExtraction := false
	foundCopy := false
	for _, progress := range progressEvents {
		switch progress.Stage {
		case stageDownloading:
			foundDownload = foundDownload || progress.Downloaded == int64(len(archive))
		case stageExtracting:
			foundExtraction = true
		case stageCopying:
			foundCopy = true
		}
	}
	if !foundDownload || !foundExtraction || !foundCopy {
		t.Fatalf("missing progress stages: %#v", progressEvents)
	}
}

func TestDownloadAndExtractRejectsInvalidSHA256(t *testing.T) {
	archive, err := os.ReadFile(filepath.Join("testdata", "basic.rar"))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write(archive)
	}))
	defer server.Close()

	destination := t.TempDir()
	err = downloadAndExtractVerified(
		context.Background(),
		server.Client(),
		server.URL,
		destination,
		strings.Repeat("0", sha256.Size*2),
		nil,
	)
	if err == nil || !strings.Contains(err.Error(), "checksum SHA-256 inválido") {
		t.Fatalf("error = %v, want SHA-256 validation error", err)
	}
	entries, err := os.ReadDir(destination)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("destination changed after checksum failure: %v", entries)
	}
}

func TestDownloadProgressIsRendered(t *testing.T) {
	model := newAppModel()
	model.screen = installScreen
	model.width = 80
	model.selectedPath = `C:\Steam\Madden NFL 27`
	model.progress = installationProgress{
		Stage:      stageDownloading,
		Downloaded: 50,
		TotalBytes: 100,
	}

	view := model.View()
	if !strings.Contains(view, "50%") || !strings.Contains(view, "50 B / 100 B") {
		t.Fatalf("download progress was not rendered: %q", view)
	}
}

func TestInstallationProgressMessageUpdatesModel(t *testing.T) {
	model := newAppModel()
	model.screen = installScreen
	model.installEvents = make(chan tea.Msg)
	progress := installationProgress{
		Stage:          stageCopying,
		CompletedFiles: 2,
		TotalFiles:     3,
	}

	updated, command := model.Update(installationProgressMsg{progress: progress})
	model = updated.(appModel)
	if model.progress != progress {
		t.Fatalf("progress = %#v, want %#v", model.progress, progress)
	}
	if command == nil {
		t.Fatal("expected command waiting for the next progress event")
	}
	close(model.installEvents)
}

func TestInjectFileBacksUpAndActivatesFixedExecutable(t *testing.T) {
	gamePath := t.TempDir()
	originalContent := []byte("original executable")
	fixedContent := []byte("fixed executable")
	if err := os.WriteFile(filepath.Join(gamePath, gameExecutableName), originalContent, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gamePath, fixedExecutableName), fixedContent, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := injectFile(gamePath); err != nil {
		t.Fatal(err)
	}
	backup, err := os.ReadFile(filepath.Join(gamePath, backupExecutableName))
	if err != nil {
		t.Fatal(err)
	}
	active, err := os.ReadFile(filepath.Join(gamePath, gameExecutableName))
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != string(originalContent) {
		t.Fatalf("backup content = %q, want %q", backup, originalContent)
	}
	if string(active) != string(fixedContent) {
		t.Fatalf("active content = %q, want %q", active, fixedContent)
	}
	if _, err := os.Stat(filepath.Join(gamePath, fixedExecutableName)); !os.IsNotExist(err) {
		t.Fatalf("fixed executable still exists after rename: %v", err)
	}
}

func TestInjectFileDoesNotChangeOriginalWhenFixedIsMissing(t *testing.T) {
	gamePath := t.TempDir()
	originalContent := []byte("original executable")
	if err := os.WriteFile(filepath.Join(gamePath, gameExecutableName), originalContent, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := injectFile(gamePath); err == nil {
		t.Fatal("expected an error for missing fixed executable")
	}
	active, err := os.ReadFile(filepath.Join(gamePath, gameExecutableName))
	if err != nil {
		t.Fatal(err)
	}
	if string(active) != string(originalContent) {
		t.Fatalf("original content changed: %q", active)
	}
	if _, err := os.Stat(filepath.Join(gamePath, backupExecutableName)); !os.IsNotExist(err) {
		t.Fatalf("backup was created despite validation failure: %v", err)
	}
}

func TestInjectFileRefusesToOverwriteExistingBackup(t *testing.T) {
	gamePath := t.TempDir()
	files := map[string]string{
		gameExecutableName:   "current",
		fixedExecutableName:  "fixed",
		backupExecutableName: "existing backup",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(gamePath, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if err := injectFile(gamePath); err == nil {
		t.Fatal("expected an error for existing backup")
	}
	for name, want := range files {
		content, err := os.ReadFile(filepath.Join(gamePath, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(content) != want {
			t.Fatalf("%s content = %q, want %q", name, content, want)
		}
	}
}

func TestInstallationFinishedChangesScreen(t *testing.T) {
	model := newAppModel()
	model.screen = installScreen
	model.previousScreen = manualPathScreen

	updated, _ := model.Update(installationFinishedMsg{})
	model = updated.(appModel)
	if model.screen != resultScreen {
		t.Fatalf("screen = %v, want resultScreen", model.screen)
	}

	model.screen = installScreen
	updated, _ = model.Update(installationFinishedMsg{err: os.ErrPermission})
	model = updated.(appModel)
	if model.screen != errorScreen || model.errorMessage == "" {
		t.Fatalf("screen = %v, error = %q", model.screen, model.errorMessage)
	}
}
