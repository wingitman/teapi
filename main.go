package main

import (
	"flag"
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
)

func main() {
	recordUpdate := flag.Bool("record-update", false, "record installed update metadata and exit")
	updateCommit := flag.String("update-commit", "", "commit to record with --record-update")
	updateRepo := flag.String("update-repo", "", "repo path to record with --record-update")
	flag.Parse()

	if *recordUpdate {
		if err := RecordUpdateMetadata(*updateCommit, *updateRepo); err != nil {
			fmt.Fprintln(os.Stderr, "Error recording update metadata:", err)
			os.Exit(1)
		}
		return
	}

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
