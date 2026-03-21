package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func getConfigDir() string {
	xdgConfig := os.Getenv("XDG_CONFIG_HOME")
	if xdgConfig != "" {
		return filepath.Join(xdgConfig, "shimsumm")
	}
	home := os.Getenv("HOME")
	if home == "" {
		fmt.Fprintf(os.Stderr, "shimsumm: Neither XDG_CONFIG_HOME nor HOME is set\n")
		os.Exit(1)
	}
	return filepath.Join(home, ".config", "shimsumm")
}

func getFiltersDir() string {
	return filepath.Join(getConfigDir(), "filters")
}

func getTestsDir() string {
	return filepath.Join(getConfigDir(), "tests")
}
