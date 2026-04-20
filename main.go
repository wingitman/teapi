package main

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
)

func main() {
	m, err := NewModel()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error initialising teapi:", err)
		os.Exit(1)
	}

	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "Error running teapi:", err)
		os.Exit(1)
	}
}
