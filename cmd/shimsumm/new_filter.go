package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func cmdNewFilter(tool string) {
	filtersDir := getFiltersDir()
	filterPath := filepath.Join(filtersDir, tool)

	if _, err := os.Stat(filterPath); err == nil {
		fmt.Fprintf(os.Stderr, "shimsumm: filter already exists: %s\n", filterPath)
		os.Exit(1)
	}

	if err := os.MkdirAll(filtersDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "shimsumm: cannot create filters directory: %v\n", err)
		os.Exit(1)
	}

	content := `#!/bin/sh
eval "$(shimsumm emit-wrap)"

smsm_filter() {
  while IFS= read -r line || [ -n "$line" ]; do
    printf '%s\n' "$line"
  done
}

smsm_wrap "$@"
`
	if err := os.WriteFile(filterPath, []byte(content), 0755); err != nil {
		fmt.Fprintf(os.Stderr, "shimsumm: cannot write filter: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("created %s\n", filterPath)
}
