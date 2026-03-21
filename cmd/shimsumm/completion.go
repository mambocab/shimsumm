package main

import (
	"os"

	"github.com/spf13/cobra"
)

func completeFilterNames(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	entries, err := os.ReadDir(getFiltersDir())
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if info, err := e.Info(); err == nil && info.Mode()&0111 != 0 {
			names = append(names, e.Name())
		}
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}
