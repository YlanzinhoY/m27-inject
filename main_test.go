package main

import (
	"context"
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
	if err := downloadAndExtract(context.Background(), server.Client(), server.URL, destination); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(destination, "link.txt")); err != nil {
		t.Fatal(err)
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
