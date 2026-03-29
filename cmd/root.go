package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/devenjarvis/baton/internal/tui"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "baton",
	Short: "A terminal-native tool for orchestrating multiple Claude Code agents",
	RunE: func(cmd *cobra.Command, args []string) error {
		ensureGitignore()
		p := tea.NewProgram(tui.NewApp())
		if _, err := p.Run(); err != nil {
			return fmt.Errorf("error running TUI: %w", err)
		}
		return nil
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// ensureGitignore adds .baton/ to .gitignore if not already present.
func ensureGitignore() {
	const entry = ".baton/"
	path := ".gitignore"

	// Check if .gitignore exists and already contains .baton/.
	if data, err := os.ReadFile(path); err == nil {
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		for scanner.Scan() {
			if strings.TrimSpace(scanner.Text()) == entry {
				return // already present
			}
		}
	}

	// Append .baton/ to .gitignore.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return // best-effort
	}
	defer f.Close()

	// Add newline before entry if file doesn't end with one.
	if info, err := f.Stat(); err == nil && info.Size() > 0 {
		buf := make([]byte, 1)
		if _, err := f.ReadAt(buf, info.Size()-1); err == nil && buf[0] != '\n' {
			f.WriteString("\n")
		}
	}
	f.WriteString(entry + "\n")
}
