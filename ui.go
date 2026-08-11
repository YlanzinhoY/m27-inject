package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type screen int

const (
	sourceScreen screen = iota
	manualPathScreen
	steamInstallationsScreen
	installScreen
	resultScreen
	errorScreen
)

type installationFinishedMsg struct {
	err error
}

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("51"))
	selectedStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("213"))
	mutedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("244"))
	successStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("42"))
	errorStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("196"))
	pathStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("229"))
)

type appModel struct {
	screen         screen
	previousScreen screen
	cursor         int
	pathInput      string
	steamPaths     []string
	selectedPath   string
	errorMessage   string
	width          int
}

func newAppModel() appModel {
	return appModel{screen: sourceScreen}
}

func (appModel) Init() tea.Cmd {
	return nil
}

func (model appModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		model.width = message.Width
		return model, nil
	case installationFinishedMsg:
		if message.err != nil {
			model.showError(model.previousScreen, message.err.Error())
			return model, nil
		}
		model.screen = resultScreen
		return model, nil
	case tea.KeyMsg:
		if message.Type == tea.KeyCtrlC {
			return model, tea.Quit
		}

		switch model.screen {
		case sourceScreen:
			return model.updateSourceScreen(message)
		case manualPathScreen:
			return model.updateManualPathScreen(message)
		case steamInstallationsScreen:
			return model.updateSteamInstallationsScreen(message)
		case resultScreen:
			if message.Type == tea.KeyEnter || message.Type == tea.KeyEsc || message.String() == "q" {
				return model, tea.Quit
			}
		case errorScreen:
			if message.Type == tea.KeyEnter || message.Type == tea.KeyEsc {
				model.screen = model.previousScreen
				model.errorMessage = ""
				return model, nil
			}
		}
	}
	return model, nil
}

func (model appModel) updateSourceScreen(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.Type {
	case tea.KeyUp:
		if model.cursor > 0 {
			model.cursor--
		}
	case tea.KeyDown:
		if model.cursor < 1 {
			model.cursor++
		}
	case tea.KeyEnter:
		if model.cursor == 0 {
			model.steamPaths = findSteamGameFolders(gameFolderName)
			switch len(model.steamPaths) {
			case 0:
				model.showError(sourceScreen, fmt.Sprintf(
					"%s não foi encontrado nas bibliotecas da Steam.", gameFolderName,
				))
			case 1:
				model.selectedPath = model.steamPaths[0]
				model.previousScreen = sourceScreen
				model.screen = installScreen
				return model, installFiles(model.selectedPath)
			default:
				model.cursor = 0
				model.screen = steamInstallationsScreen
			}
			return model, nil
		}

		model.pathInput = ""
		model.screen = manualPathScreen
		return model, nil
	case tea.KeyEsc:
		return model, tea.Quit
	}
	return model, nil
}

func (model appModel) updateManualPathScreen(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.Type {
	case tea.KeyEsc:
		model.screen = sourceScreen
		model.cursor = 1
		return model, nil
	case tea.KeyEnter:
		path, err := validateGamePath(model.pathInput)
		if err != nil {
			model.showError(manualPathScreen, err.Error())
			return model, nil
		}
		model.selectedPath = path
		model.previousScreen = manualPathScreen
		model.screen = installScreen
		return model, installFiles(model.selectedPath)
	case tea.KeyBackspace, tea.KeyDelete:
		runes := []rune(model.pathInput)
		if len(runes) > 0 {
			model.pathInput = string(runes[:len(runes)-1])
		}
		return model, nil
	case tea.KeyCtrlU:
		model.pathInput = ""
		return model, nil
	case tea.KeyRunes:
		model.pathInput += string(key.Runes)
		return model, nil
	}
	return model, nil
}

func (model appModel) updateSteamInstallationsScreen(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.Type {
	case tea.KeyUp:
		if model.cursor > 0 {
			model.cursor--
		}
	case tea.KeyDown:
		if model.cursor < len(model.steamPaths)-1 {
			model.cursor++
		}
	case tea.KeyEnter:
		model.selectedPath = model.steamPaths[model.cursor]
		model.previousScreen = steamInstallationsScreen
		model.screen = installScreen
		return model, installFiles(model.selectedPath)
	case tea.KeyEsc:
		model.cursor = 0
		model.screen = sourceScreen
	}
	return model, nil
}

func installFiles(gamePath string) tea.Cmd {
	return func() tea.Msg {
		return installationFinishedMsg{err: downloadFiles(gamePath)}
	}
}

func (model *appModel) showError(previous screen, message string) {
	model.previousScreen = previous
	model.errorMessage = message
	model.screen = errorScreen
}

func (model appModel) View() string {
	var content string
	switch model.screen {
	case sourceScreen:
		content = model.sourceView()
	case manualPathScreen:
		content = model.manualPathView()
	case steamInstallationsScreen:
		content = model.steamInstallationsView()
	case installScreen:
		content = model.installView()
	case resultScreen:
		content = model.resultView()
	case errorScreen:
		content = model.errorView()
	}

	container := lipgloss.NewStyle().Padding(1, 2)
	if model.width > 0 {
		container = container.MaxWidth(model.width)
	}
	return container.Render(content)
}

func (model appModel) sourceView() string {
	options := []string{
		"Procurar automaticamente na Steam",
		"Informar o caminho completo",
	}

	var builder strings.Builder
	builder.WriteString(titleStyle.Render("MADDEN NFL 27"))
	builder.WriteString("\n")
	builder.WriteString(mutedStyle.Render("Localizador de instalação"))
	builder.WriteString("\n\nOnde o jogo está instalado?\n\n")
	for index, option := range options {
		prefix := "  "
		style := lipgloss.NewStyle()
		if index == model.cursor {
			prefix = "› "
			style = selectedStyle
		}
		builder.WriteString(style.Render(prefix + option))
		builder.WriteString("\n")
	}
	builder.WriteString("\n")
	builder.WriteString(mutedStyle.Render("↑/↓ navegar • Enter selecionar • Esc sair"))
	return builder.String()
}

func (model appModel) manualPathView() string {
	input := model.pathInput
	if input == "" {
		input = mutedStyle.Render(`C:\SteamLibrary\steamapps\common\Madden NFL 27`)
	} else {
		input = pathStyle.Render(input)
	}

	return titleStyle.Render("CAMINHO DO JOGO") +
		"\n\nCole ou digite o caminho completo:\n\n" +
		selectedStyle.Render("> ") + input + selectedStyle.Render("█") +
		"\n\n" + mutedStyle.Render("Enter confirmar • Ctrl+U limpar • Esc voltar")
}

func (model appModel) steamInstallationsView() string {
	var builder strings.Builder
	builder.WriteString(titleStyle.Render("INSTALAÇÕES ENCONTRADAS"))
	builder.WriteString("\n\n")
	for index, path := range model.steamPaths {
		prefix := "  "
		style := pathStyle
		if index == model.cursor {
			prefix = "› "
			style = selectedStyle
		}
		builder.WriteString(style.Render(prefix + path))
		builder.WriteString("\n")
	}
	builder.WriteString("\n")
	builder.WriteString(mutedStyle.Render("↑/↓ navegar • Enter selecionar • Esc voltar"))
	return builder.String()
}

func (model appModel) resultView() string {
	return successStyle.Render("ARQUIVOS INSTALADOS") +
		"\n\n" + pathStyle.Render(model.selectedPath) +
		"\n\n" + mutedStyle.Render("Download concluído e conteúdo do RAR extraído na pasta do Madden.") +
		"\n\n" + mutedStyle.Render("Enter ou q para sair")
}

func (model appModel) installView() string {
	return titleStyle.Render("BAIXANDO E INSTALANDO") +
		"\n\n" + pathStyle.Render(model.selectedPath) +
		"\n\n" + mutedStyle.Render("Aguarde enquanto o arquivo RAR é baixado e extraído...") +
		"\n\n" + mutedStyle.Render("Ctrl+C para cancelar")
}

func (model appModel) errorView() string {
	return errorStyle.Render("NÃO FOI POSSÍVEL CONTINUAR") +
		"\n\n" + model.errorMessage +
		"\n\n" + mutedStyle.Render("Enter ou Esc para voltar")
}
