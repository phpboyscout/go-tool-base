package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	root := &cobra.Command{
		Use:   "changelog",
		Short: "Generate changelogs from git history",
		Long:  "A pure-Go changelog generator that reads conventional commits from git history and produces CHANGELOG.md.",
	}

	root.AddCommand(newGenerateCmd())

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
