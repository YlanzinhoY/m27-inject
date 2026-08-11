package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

func main() {
	if err := newRootCommand().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Erro: %v\n", err)
		os.Exit(1)
	}
}

func newRootCommand() *cobra.Command {
	command := &cobra.Command{
		Use:           "madden-27-inject",
		Short:         "Localiza a instalação do Madden NFL 27",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(command *cobra.Command, _ []string) error {
			program := tea.NewProgram(newAppModel(), tea.WithAltScreen())
			finalModel, err := program.Run()
			if err != nil {
				return fmt.Errorf("não foi possível abrir a interface: %w", err)
			}

			model, ok := finalModel.(appModel)
			if ok && model.screen == resultScreen && model.selectedPath != "" {
				fmt.Fprintf(command.OutOrStdout(), "Arquivos instalados em: %s\n", model.selectedPath)
			}
			return nil
		},
	}
	command.CompletionOptions.DisableDefaultCmd = true
	return command
}
