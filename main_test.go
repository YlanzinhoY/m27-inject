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
	"github.com/spf13/cobra"
)

func TestExplorerMousetrapIsDisabled(t *testing.T) {
	previousHelpText := cobra.MousetrapHelpText
	defer func() {
		cobra.MousetrapHelpText = previousHelpText
	}()

	cobra.MousetrapHelpText = "command-line warning"
	disableExplorerMousetrap()
	if cobra.MousetrapHelpText != "" {
		t.Fatalf("MousetrapHelpText = %q, want empty", cobra.MousetrapHelpText)
	}
}

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

func TestEndToEndInstallsFixedExecutableWithoutDuplicates(t *testing.T) {
	archive, err := os.ReadFile(filepath.Join("testdata", "install.rar"))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/vnd.rar")
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write(archive)
	}))
	defer server.Close()

	gamePath := filepath.Join(t.TempDir(), gameFolderName)
	if err := os.Mkdir(gamePath, 0o755); err != nil {
		t.Fatal(err)
	}
	originalContent := []byte("fake original Madden27.exe")
	if err := os.WriteFile(filepath.Join(gamePath, gameExecutableName), originalContent, 0o600); err != nil {
		t.Fatal(err)
	}

	checksum := sha256.Sum256(archive)
	var stages []installationStage
	err = downloadAndInstallVerified(
		context.Background(),
		server.Client(),
		server.URL,
		gamePath,
		fmt.Sprintf("%x", checksum[:]),
		func(progress installationProgress) {
			stages = append(stages, progress.Stage)
		},
	)
	if err != nil {
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
	if string(active) != "fixed executable fixture\n" {
		t.Fatalf("active executable content = %q", active)
	}
	if _, err := os.Stat(filepath.Join(gamePath, fixedExecutableName)); !os.IsNotExist(err) {
		t.Fatalf("fixed executable was duplicated in the game folder: %v", err)
	}

	entries, err := os.ReadDir(gamePath)
	if err != nil {
		t.Fatal(err)
	}
	wantNames := []string{gameExecutableName, backupExecutableName, "version.dll"}
	if len(entries) != len(wantNames) {
		t.Fatalf("game folder contains %d entries, want %d: %v", len(entries), len(wantNames), entries)
	}
	for index, want := range wantNames {
		if entries[index].Name() != want {
			t.Fatalf("entry %d = %q, want %q", index, entries[index].Name(), want)
		}
	}

	injectIndex := firstStageIndex(stages, stageInjecting)
	copyIndex := firstStageIndex(stages, stageCopying)
	if injectIndex < 0 || copyIndex < 0 || injectIndex >= copyIndex {
		t.Fatalf("stages = %v, want injection before copy", stages)
	}
}

func firstStageIndex(stages []installationStage, wanted installationStage) int {
	for index, stage := range stages {
		if stage == wanted {
			return index
		}
	}
	return -1
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

func TestExtractionProgressDoesNotRenderZeroFiles(t *testing.T) {
	model := newAppModel()
	model.screen = installScreen
	model.selectedPath = `C:\Steam\Madden NFL 27`
	model.progress = installationProgress{Stage: stageExtracting}

	view := model.View()
	if !strings.Contains(view, "Extraindo o arquivo RAR...") {
		t.Fatalf("extraction status was not rendered: %q", view)
	}
	if strings.Contains(view, "0 arquivo") {
		t.Fatalf("zero file count should be hidden: %q", view)
	}

	model.progress.CompletedFiles = 2
	view = model.View()
	if !strings.Contains(view, "2 arquivos extraídos") {
		t.Fatalf("completed extraction count was not rendered: %q", view)
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

func TestPrepareExecutableInjectionRenamesFilesBeforeCopy(t *testing.T) {
	gamePath := t.TempDir()
	stagingPath := t.TempDir()
	originalContent := []byte("original executable")
	fixedContent := []byte("fixed executable")
	if err := os.WriteFile(filepath.Join(gamePath, gameExecutableName), originalContent, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stagingPath, fixedExecutableName), fixedContent, 0o600); err != nil {
		t.Fatal(err)
	}

	injection, err := prepareExecutableInjection(gamePath, stagingPath)
	if err != nil {
		t.Fatal(err)
	}
	backup, err := os.ReadFile(filepath.Join(gamePath, backupExecutableName))
	if err != nil {
		t.Fatal(err)
	}
	staged, err := os.ReadFile(filepath.Join(stagingPath, gameExecutableName))
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != string(originalContent) {
		t.Fatalf("backup content = %q, want %q", backup, originalContent)
	}
	if string(staged) != string(fixedContent) {
		t.Fatalf("staged content = %q, want %q", staged, fixedContent)
	}
	if _, err := os.Stat(filepath.Join(gamePath, gameExecutableName)); !os.IsNotExist(err) {
		t.Fatalf("game executable was copied before the backup step: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stagingPath, fixedExecutableName)); !os.IsNotExist(err) {
		t.Fatalf("fixed executable still has its old staging name: %v", err)
	}
	if err := injection.rollback(); err != nil {
		t.Fatal(err)
	}
}

func TestInstallExtractedFilesBacksUpThenCopiesFixedExecutable(t *testing.T) {
	gamePath := t.TempDir()
	stagingPath := t.TempDir()
	originalContent := []byte("original executable")
	fixedContent := []byte("fixed executable")
	if err := os.WriteFile(filepath.Join(gamePath, gameExecutableName), originalContent, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stagingPath, fixedExecutableName), fixedContent, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stagingPath, "version.dll"), []byte("dll"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stages []installationStage
	err := installExtractedFiles(stagingPath, gamePath, 2, func(progress installationProgress) {
		stages = append(stages, progress.Stage)
	})
	if err != nil {
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
	if string(backup) != string(originalContent) || string(active) != string(fixedContent) {
		t.Fatalf("backup = %q, active = %q", backup, active)
	}
	if len(stages) < 2 || stages[0] != stageInjecting || stages[1] != stageCopying {
		t.Fatalf("stages = %v, want injection before copy", stages)
	}
}

func TestPrepareExecutableInjectionDoesNotChangeOriginalWhenFixedIsMissing(t *testing.T) {
	gamePath := t.TempDir()
	stagingPath := t.TempDir()
	originalContent := []byte("original executable")
	if err := os.WriteFile(filepath.Join(gamePath, gameExecutableName), originalContent, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := prepareExecutableInjection(gamePath, stagingPath); err == nil {
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

func TestPrepareExecutableInjectionRefusesToOverwriteExistingBackup(t *testing.T) {
	gamePath := t.TempDir()
	stagingPath := t.TempDir()
	gameFiles := map[string]string{
		gameExecutableName:   "current",
		backupExecutableName: "existing backup",
	}
	for name, content := range gameFiles {
		if err := os.WriteFile(filepath.Join(gamePath, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(stagingPath, fixedExecutableName), []byte("fixed"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := prepareExecutableInjection(gamePath, stagingPath); err == nil {
		t.Fatal("expected an error for existing backup")
	}
	for name, want := range gameFiles {
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
