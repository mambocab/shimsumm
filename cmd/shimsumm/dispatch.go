package main

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

func cmdDispatch(tool string, args []string) {
	filtersDir := getFiltersDir()
	filterPath := filepath.Join(filtersDir, tool)

	stat, err := os.Stat(filterPath)
	if err != nil || (stat.Mode()&0111) == 0 {
		fmt.Fprintf(os.Stderr, "shimsumm: no filter for \"%s\" in %s\n", tool, filtersDir)
		os.Exit(127)
	}

	// Use syscall.Exec to replace the current process (no subprocess overhead)
	execArgs := append([]string{filterPath}, args...)
	err = syscall.Exec(filterPath, execArgs, os.Environ())
	if err != nil {
		fmt.Fprintf(os.Stderr, "shimsumm: exec failed: %v\n", err)
		os.Exit(1)
	}
}
