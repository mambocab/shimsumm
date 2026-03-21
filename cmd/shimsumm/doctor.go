package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

var validDoctorChecks = map[string]bool{
	"executable":      true,
	"shebang":         true,
	"sources-wrap":    true,
	"calls-wrap":      true,
	"syntax":          true,
	"sources-cleanly": true,
}

func parseSkipChecks(filePath string) (map[string]bool, []string) {
	skips := map[string]bool{}
	var warnings []string

	data, err := os.ReadFile(filePath)
	if err != nil {
		return skips, warnings
	}

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		const prefix = "# shimsumm-doctor: skip "
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		rest := strings.TrimPrefix(line, prefix)
		for _, name := range strings.Split(rest, ",") {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			if validDoctorChecks[name] {
				skips[name] = true
			} else {
				warnings = append(warnings, fmt.Sprintf("%s: WARN: unknown check name in skip comment: %s", filepath.Base(filePath), name))
			}
		}
	}

	return skips, warnings
}

type checkResult struct {
	name    string
	status  string // "OK", "FAIL", "SKIP"
	message string
}

func cmdDoctor(verbose bool) {
	filtersDir := getFiltersDir()
	pathEnv := os.Getenv("PATH")

	envFail := false
	var envResults []checkResult

	// ENV check 1: filters directory exists
	if _, err := os.Stat(filtersDir); err == nil {
		envResults = append(envResults, checkResult{"ENV", "OK", "filters directory exists"})
	} else {
		envResults = append(envResults, checkResult{"ENV", "FAIL", fmt.Sprintf("filters directory missing: %s", filtersDir)})
		envFail = true
	}

	// ENV check 2: filters directory on PATH
	inPath := false
	for _, p := range filepath.SplitList(pathEnv) {
		if p == filtersDir {
			inPath = true
			break
		}
	}
	if inPath {
		envResults = append(envResults, checkResult{"ENV", "OK", "filters directory on PATH"})
	} else {
		envResults = append(envResults, checkResult{"ENV", "FAIL", "filters directory not on PATH"})
		envFail = true
	}

	// ENV check 3: shimsumm on PATH
	if _, err := exec.LookPath("shimsumm"); err == nil {
		envResults = append(envResults, checkResult{"ENV", "OK", "shimsumm on PATH"})
	} else {
		envResults = append(envResults, checkResult{"ENV", "FAIL", "shimsumm not found on PATH"})
		envFail = true
	}

	// Print ENV results
	for _, r := range envResults {
		if verbose || r.status == "FAIL" {
			fmt.Printf("%s: %s: %s\n", r.name, r.status, r.message)
		}
	}

	// Per-filter checks
	total := 0
	passed := 0

	entries, err := os.ReadDir(filtersDir)
	if err != nil {
		entries = nil
	}

	var filterNames []string
	for _, entry := range entries {
		if !entry.IsDir() {
			filterNames = append(filterNames, entry.Name())
		}
	}
	sort.Strings(filterNames)

	for _, name := range filterNames {
		filterPath := filepath.Join(filtersDir, name)
		total++

		skips, warnings := parseSkipChecks(filterPath)
		for _, w := range warnings {
			fmt.Println(w)
		}

		var checks []checkResult
		thisFailed := false

		// Check: executable
		if skips["executable"] {
			checks = append(checks, checkResult{name, "SKIP", "executable"})
		} else if stat, err := os.Stat(filterPath); err == nil && (stat.Mode()&0111) != 0 {
			checks = append(checks, checkResult{name, "OK", "executable"})
		} else {
			checks = append(checks, checkResult{name, "FAIL", "not executable"})
			thisFailed = true
		}

		// Read file content for text-based checks
		content, _ := os.ReadFile(filterPath)
		contentStr := string(content)
		lines := strings.Split(contentStr, "\n")

		// Check: shebang
		if skips["shebang"] {
			checks = append(checks, checkResult{name, "SKIP", "shebang"})
		} else if len(lines) > 0 && strings.HasPrefix(lines[0], "#!") {
			checks = append(checks, checkResult{name, "OK", "shebang present"})
		} else {
			checks = append(checks, checkResult{name, "FAIL", "no shebang"})
			thisFailed = true
		}

		// Check: sources-wrap
		if skips["sources-wrap"] {
			checks = append(checks, checkResult{name, "SKIP", "sources shimsumm emit-wrap"})
		} else if strings.Contains(contentStr, "shimsumm emit-wrap") {
			checks = append(checks, checkResult{name, "OK", "sources shimsumm emit-wrap"})
		} else {
			checks = append(checks, checkResult{name, "FAIL", "does not source shimsumm emit-wrap"})
			thisFailed = true
		}

		// Check: calls-wrap
		if skips["calls-wrap"] {
			checks = append(checks, checkResult{name, "SKIP", "calls smsm_wrap"})
		} else if strings.Contains(contentStr, `smsm_wrap "$@"`) {
			checks = append(checks, checkResult{name, "OK", "calls smsm_wrap"})
		} else {
			checks = append(checks, checkResult{name, "FAIL", `does not call smsm_wrap "$@"`})
			thisFailed = true
		}

		// Check: syntax
		if skips["syntax"] {
			checks = append(checks, checkResult{name, "SKIP", "syntax"})
		} else if exec.Command("sh", "-n", filterPath).Run() == nil {
			checks = append(checks, checkResult{name, "OK", "syntax ok"})
		} else {
			checks = append(checks, checkResult{name, "FAIL", "syntax error"})
			thisFailed = true
		}

		// Check: sources-cleanly
		if skips["sources-cleanly"] {
			checks = append(checks, checkResult{name, "SKIP", "sources cleanly"})
		} else {
			stubDir, _ := os.MkdirTemp("", "shimsumm-doctor-")
			stubScript := "#!/bin/sh\ncase \"$1\" in\n  emit-wrap) printf 'smsm_wrap() { return 0; }\\n' ;;\nesac\n"
			os.WriteFile(filepath.Join(stubDir, "shimsumm"), []byte(stubScript), 0755)

			testCmd := exec.Command("sh", "-c", fmt.Sprintf(". %q", filterPath))
			var env []string
			for _, e := range os.Environ() {
				if !strings.HasPrefix(e, "PATH=") {
					env = append(env, e)
				}
			}
			env = append(env, fmt.Sprintf("PATH=%s:%s", stubDir, pathEnv))
			testCmd.Env = env

			if testCmd.Run() == nil {
				checks = append(checks, checkResult{name, "OK", "sources cleanly"})
			} else {
				checks = append(checks, checkResult{name, "FAIL", "source error"})
				thisFailed = true
			}
			os.RemoveAll(stubDir)
		}

		// Output
		if verbose {
			for _, c := range checks {
				fmt.Printf("%s: %s: %s\n", c.name, c.status, c.message)
			}
		} else if thisFailed {
			for _, c := range checks {
				if c.status == "FAIL" {
					fmt.Printf("%s: FAIL: %s\n", c.name, c.message)
				}
			}
		} else {
			allSkipped := true
			for _, c := range checks {
				if c.status != "SKIP" {
					allSkipped = false
					break
				}
			}
			if allSkipped {
				fmt.Printf("%s: SKIP\n", name)
			} else {
				fmt.Printf("%s: OK\n", name)
			}
		}

		if !thisFailed {
			passed++
		}
	}

	failed := total - passed
	fmt.Printf("%d filters checked, %d passed, %d failed\n", total, passed, failed)

	if envFail || failed > 0 {
		os.Exit(1)
	}
}
