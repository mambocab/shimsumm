package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
)

// ---- Config Discovery ----

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

// ---- Emit smsm_wrap ----

func emitSmsmWrap() string {
	return `smsm_wrap() {
  # Extract tool name and filters directory from script path.
  # ${0##*/} = basename, ${0%/*} = dirname
  # Note: assumes $0 is resolved by shell when filter invoked via PATH.
  # For this to work correctly, filters directory must be in PATH and shell
  # resolves the full path. If filter is invoked with explicit path, $0 will
  # contain that path and dirname will extract it correctly.
  _smsm_tool="${0##*/}"
  _smsm_filters_dir="${0%/*}"

  # Find real tool binary in PATH, but skip anything before filters_dir.
  # This ensures filters dir is checked first, then the real binary is found after.
  # If filters_dir is not in PATH, start looking from the beginning.
  _smsm_found_filters_dir=0
  _smsm_real=""
  _smsm_saved_ifs="$IFS"

  # Check if filters_dir is in PATH; if not, we'll search from the start
  case ":$PATH:" in
    *":$_smsm_filters_dir:"*) ;;
    *) _smsm_found_filters_dir=1 ;;  # Not in PATH, start searching immediately
  esac

  IFS=:
  for _smsm_entry in $PATH; do
    IFS="$_smsm_saved_ifs"

    # Once we've seen filters_dir, start looking for real binaries
    if [ "$_smsm_found_filters_dir" = "1" ] && [ -x "$_smsm_entry/$_smsm_tool" ]; then
      _smsm_real="$_smsm_entry/$_smsm_tool"
      break
    fi

    # Mark when we've seen filters_dir in PATH
    if [ "$_smsm_entry" = "$_smsm_filters_dir" ]; then
      _smsm_found_filters_dir=1
    fi

    IFS=:
  done
  IFS="$_smsm_saved_ifs"

  # Bail if real tool not found
  if [ -z "$_smsm_real" ]; then
    printf 'shimsumm: real %s not found in PATH\n' "$_smsm_tool" >&2
    unset _smsm_tool _smsm_filters_dir _smsm_found_filters_dir _smsm_real
    unset _smsm_saved_ifs _smsm_entry
    return 127
  fi

  # Check --only-shim / --dont-shim exclusion lists
  _smsm_skip=0
  if [ -n "${SHIMSUMM_ONLY_SHIM:-}" ]; then
    case ":${SHIMSUMM_ONLY_SHIM}:" in
      *":${_smsm_tool}:"*) ;;
      *) _smsm_skip=1 ;;
    esac
  fi
  if [ -n "${SHIMSUMM_DONT_SHIM:-}" ]; then
    case ":${SHIMSUMM_DONT_SHIM}:" in
      *":${_smsm_tool}:"*) _smsm_skip=1 ;;
    esac
  fi
  if [ "$_smsm_skip" = "1" ]; then
    unset _smsm_tool _smsm_filters_dir _smsm_found_filters_dir
    unset _smsm_saved_ifs _smsm_entry _smsm_skip
    exec "$_smsm_real" "$@"
  fi
  unset _smsm_skip

  # Create temp file for full unfiltered output with timestamp naming
  mkdir -p /tmp/shimsumm
  _smsm_timestamp=$(date +%Y%m%d%H%M%S)
  _smsm_rand=$((RANDOM % 1000))
  _smsm_outfile="/tmp/shimsumm/${_smsm_tool}-${_smsm_timestamp}-${_smsm_rand}"
  touch "$_smsm_outfile"

  # Define default passthrough filter if not already defined
  command -v smsm_filter >/dev/null 2>&1 || \
    smsm_filter() {
      while IFS= read -r _smsm_line || [ -n "$_smsm_line" ]; do
        printf '%s\n' "$_smsm_line"
      done
    }

  # Run real tool, capture stdout+stderr to temp file
  # (redirected at shell level so both streams are merged with true interleaving)
  "$_smsm_real" "$@" > "$_smsm_outfile" 2>&1
  _smsm_exit_code=$?

  # Filter the output from the temp file
  # (reading from file avoids SIGPIPE issues with early filter exit)
  smsm_filter < "$_smsm_outfile"

  # Append annotation so user can access full output if needed
  printf '[full output: %s]\n' "$_smsm_outfile"

  # Clean up locals
  unset _smsm_tool _smsm_filters_dir _smsm_found_filters_dir _smsm_real
  unset _smsm_saved_ifs _smsm_entry _smsm_outfile _smsm_line
  unset _smsm_timestamp _smsm_rand

  # Return original exit code (DO NOT unset before return!)
  return "$_smsm_exit_code"
}`
}

// ---- Test Runner ----

func runFilterTest(filterName, caseName, filtersDir, testsDir string) (bool, string) {
	label := filterName + "/" + caseName
	filterPath := filepath.Join(filtersDir, filterName)
	caseDir := filepath.Join(testsDir, filterName)
	inputFile := filepath.Join(caseDir, caseName+".input")
	expectedFile := filepath.Join(caseDir, caseName+".expected")
	exitFile := filepath.Join(caseDir, caseName+".exit")
	argsFile := filepath.Join(caseDir, caseName+".args")

	// Check if filter exists and is executable
	stat, err := os.Stat(filterPath)
	if err != nil || (stat.Mode()&0111) == 0 {
		return false, fmt.Sprintf("FAIL: %s\nFilter not found or not executable", label)
	}

	// Read expected output
	expectedBytes, err := os.ReadFile(expectedFile)
	if err != nil {
		return false, fmt.Sprintf("FAIL: %s\n%v", label, err)
	}
	expected := strings.TrimRight(string(expectedBytes), "\n")

	// Read expected exit code
	expectedExitCode := 0
	expectNonzero := false
	if exitBytes, err := os.ReadFile(exitFile); err == nil {
		exitStr := strings.TrimSpace(string(exitBytes))
		if exitStr == "nonzero" {
			expectNonzero = true
		} else {
			n, err := strconv.Atoi(exitStr)
			if err != nil {
				return false, fmt.Sprintf("FAIL: %s\nInvalid exit code in %s: %s", label, exitFile, exitStr)
			}
			expectedExitCode = n
		}
	}

	// Read args
	var filterArgs []string
	if argsBytes, err := os.ReadFile(argsFile); err == nil {
		argsStr := strings.TrimSpace(string(argsBytes))
		if argsStr != "" {
			filterArgs = strings.Fields(argsStr)
		}
	}

	// Determine mock exit code
	mockExitCode := expectedExitCode
	if expectNonzero {
		mockExitCode = 1
	}

	// Create mock binary
	mockDir, err := os.MkdirTemp("", "shimsumm-test-")
	if err != nil {
		return false, fmt.Sprintf("FAIL: %s\n%v", label, err)
	}
	defer os.RemoveAll(mockDir)

	mockTool := filepath.Join(mockDir, filterName)
	mockScript := fmt.Sprintf("#!/bin/sh\ncat %q\nexit %d\n", inputFile, mockExitCode)
	if err := os.WriteFile(mockTool, []byte(mockScript), 0755); err != nil {
		return false, fmt.Sprintf("FAIL: %s\n%v", label, err)
	}

	// Execute filter with mock tool in PATH
	cmd := exec.Command(filterPath, filterArgs...)
	pathEnv := fmt.Sprintf("%s:%s:%s", filtersDir, mockDir, os.Getenv("PATH"))
	cmd.Env = append(os.Environ(), fmt.Sprintf("PATH=%s", pathEnv))

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	cmdErr := cmd.Run()

	// Get actual exit code
	actualExitCode := 0
	if cmdErr != nil {
		if exitErr, ok := cmdErr.(*exec.ExitError); ok {
			actualExitCode = exitErr.ExitCode()
		}
	}

	// Filter out [full output:...] lines
	actualLines := []string{}
	scanner := bufio.NewScanner(strings.NewReader(stdout.String()))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "[full output:") {
			actualLines = append(actualLines, line)
		}
	}
	actual := strings.TrimRight(strings.Join(actualLines, "\n"), "\n")

	// Check results
	outputMatch := actual == expected
	var exitMatch bool
	if expectNonzero {
		exitMatch = actualExitCode != 0
	} else {
		exitMatch = actualExitCode == expectedExitCode
	}

	if outputMatch && exitMatch {
		return true, fmt.Sprintf("PASS: %s", label)
	}

	var result strings.Builder
	fmt.Fprintf(&result, "FAIL: %s\n", label)
	if !outputMatch {
		expectedSplitLines := strings.Split(expected, "\n")
		actualSplitLines := strings.Split(actual, "\n")
		result.WriteString(generateUnifiedDiff(expectedFile, "actual", expectedSplitLines, actualSplitLines))
	}
	if !exitMatch {
		if expectNonzero {
			fmt.Fprintf(&result, "expected nonzero exit code, got 0\n")
		} else {
			fmt.Fprintf(&result, "expected exit code %d, got %d\n", expectedExitCode, actualExitCode)
		}
	}
	return false, result.String()
}

// ---- Subcommand Handlers ----

func cmdInit(shell string, dontShim, onlyShim []string) {
	filtersDir := getFiltersDir()

	var code string
	if shell == "fish" {
		code = `set -l _smsm_f
if set -q XDG_CONFIG_HOME
    set _smsm_f "$XDG_CONFIG_HOME/shimsumm/filters"
else
    set _smsm_f "$HOME/.config/shimsumm/filters"
end
contains -- $_smsm_f $PATH; or set -gx PATH $_smsm_f $PATH
set -e _smsm_f`
		if len(dontShim) > 0 {
			code += fmt.Sprintf("\nset -gx SHIMSUMM_DONT_SHIM %q", strings.Join(dontShim, ":"))
		}
		if len(onlyShim) > 0 {
			code += fmt.Sprintf("\nset -gx SHIMSUMM_ONLY_SHIM %q", strings.Join(onlyShim, ":"))
		}
	} else {
		code = fmt.Sprintf(`_smsm_filters="%s"
case ":${PATH}:" in
  *":${_smsm_filters}:"*) ;;
  *) PATH="${_smsm_filters}:${PATH}"; export PATH ;;
esac
unset _smsm_filters`, filtersDir)
		if len(dontShim) > 0 {
			code += fmt.Sprintf("\nSHIMSUMM_DONT_SHIM=%q; export SHIMSUMM_DONT_SHIM", strings.Join(dontShim, ":"))
		}
		if len(onlyShim) > 0 {
			code += fmt.Sprintf("\nSHIMSUMM_ONLY_SHIM=%q; export SHIMSUMM_ONLY_SHIM", strings.Join(onlyShim, ":"))
		}
	}

	fmt.Println(code)
}

func cmdWrap() {
	fmt.Println(emitSmsmWrap())
}

type testCase struct {
	filter string
	name   string
}

func discoverTestCases(testsDir, filterName string) ([]testCase, []string) {
	var cases []testCase
	var warnings []string

	var filterDirs []string
	if filterName != "" {
		filterDirs = []string{filterName}
	} else {
		entries, err := os.ReadDir(testsDir)
		if err != nil {
			return nil, nil
		}
		for _, e := range entries {
			if e.IsDir() {
				filterDirs = append(filterDirs, e.Name())
			}
		}
		sort.Strings(filterDirs)
	}

	for _, f := range filterDirs {
		caseDir := filepath.Join(testsDir, f)
		entries, err := os.ReadDir(caseDir)
		if err != nil {
			continue
		}

		var caseNames []string
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".input") {
				caseName := strings.TrimSuffix(e.Name(), ".input")
				expectedFile := filepath.Join(caseDir, caseName+".expected")
				if _, err := os.Stat(expectedFile); err == nil {
					caseNames = append(caseNames, caseName)
				} else {
					warnings = append(warnings, fmt.Sprintf("warning: %s/%s.input has no matching .expected file, skipping", f, caseName))
				}
			}
		}
		sort.Strings(caseNames)
		for _, c := range caseNames {
			cases = append(cases, testCase{filter: f, name: c})
		}
	}

	return cases, warnings
}

func cmdTestRun(filterName string) {
	filtersDir := getFiltersDir()
	testsDir := getTestsDir()

	if _, err := os.Stat(testsDir); err != nil {
		fmt.Fprintf(os.Stderr, "No tests directory: %s\n", testsDir)
		os.Exit(1)
	}

	cases, warnings := discoverTestCases(testsDir, filterName)
	for _, w := range warnings {
		fmt.Fprintln(os.Stderr, w)
	}

	if len(cases) == 0 {
		fmt.Fprintf(os.Stderr, "no test cases found\n")
		os.Exit(1)
	}

	allPassed := true
	for _, tc := range cases {
		passed, output := runFilterTest(tc.filter, tc.name, filtersDir, testsDir)
		fmt.Println(output)
		if !passed {
			allPassed = false
		}
	}

	if !allPassed {
		os.Exit(1)
	}
}

func cmdTestList(filterName string, showAll bool, jsonOutput bool) {
	filtersDir := getFiltersDir()
	testsDir := getTestsDir()

	type listCase struct {
		Name string `json:"name"`
		Args bool   `json:"args,omitempty"`
		Exit string `json:"exit,omitempty"`
	}
	type listFilter struct {
		Filter string     `json:"filter"`
		Cases  []listCase `json:"cases"`
	}

	var filters []listFilter

	var filterDirs []string
	if filterName != "" {
		filterDirs = []string{filterName}
	} else {
		if entries, err := os.ReadDir(testsDir); err == nil {
			for _, e := range entries {
				if e.IsDir() {
					filterDirs = append(filterDirs, e.Name())
				}
			}
		}
		sort.Strings(filterDirs)
	}

	testedFilters := map[string]bool{}
	for _, f := range filterDirs {
		testedFilters[f] = true
		caseDir := filepath.Join(testsDir, f)
		entries, err := os.ReadDir(caseDir)
		if err != nil {
			continue
		}

		var cases []listCase
		var caseNames []string
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".input") {
				cn := strings.TrimSuffix(e.Name(), ".input")
				expectedFile := filepath.Join(caseDir, cn+".expected")
				if _, err := os.Stat(expectedFile); err == nil {
					caseNames = append(caseNames, cn)
				}
			}
		}
		sort.Strings(caseNames)

		for _, cn := range caseNames {
			lc := listCase{Name: cn}
			if _, err := os.Stat(filepath.Join(caseDir, cn+".args")); err == nil {
				lc.Args = true
			}
			if exitBytes, err := os.ReadFile(filepath.Join(caseDir, cn+".exit")); err == nil {
				lc.Exit = strings.TrimSpace(string(exitBytes))
			}
			cases = append(cases, lc)
		}
		filters = append(filters, listFilter{Filter: f, Cases: cases})
	}

	// For --all or --json, include untested filters
	if showAll || jsonOutput {
		if entries, err := os.ReadDir(filtersDir); err == nil {
			var untested []string
			for _, e := range entries {
				if !e.IsDir() && !testedFilters[e.Name()] {
					untested = append(untested, e.Name())
				}
			}
			sort.Strings(untested)
			for _, f := range untested {
				filters = append(filters, listFilter{Filter: f, Cases: []listCase{}})
			}
		}
	}

	if jsonOutput {
		data, _ := json.Marshal(filters)
		fmt.Println(string(data))
		return
	}

	// Human-readable output
	for i, f := range filters {
		if len(f.Cases) == 0 {
			fmt.Printf("%s (no tests)\n", f.Filter)
		} else {
			fmt.Println(f.Filter)
			for _, c := range f.Cases {
				annotations := []string{}
				if c.Args {
					annotations = append(annotations, "args")
				}
				if c.Exit != "" {
					annotations = append(annotations, "exit: "+c.Exit)
				}
				if len(annotations) > 0 {
					fmt.Printf("  %s (%s)\n", c.Name, strings.Join(annotations, ", "))
				} else {
					fmt.Printf("  %s\n", c.Name)
				}
			}
		}
		if i < len(filters)-1 {
			fmt.Println()
		}
	}
}

func cmdTestAdd(filterName, caseName, fromFile, argsFlag string, runCmd []string) {
	testsDir := getTestsDir()
	caseDir := filepath.Join(testsDir, filterName)
	inputFile := filepath.Join(caseDir, caseName+".input")

	// Check if case already exists
	if _, err := os.Stat(inputFile); err == nil {
		fmt.Fprintf(os.Stderr, "error: test case already exists: %s/%s\n", filterName, caseName)
		os.Exit(1)
	}

	// Ensure case directory exists
	if err := os.MkdirAll(caseDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot create directory: %v\n", err)
		os.Exit(1)
	}

	var inputData []byte
	var exitCode string
	var argsValue string
	var createdFiles []string

	if len(runCmd) > 0 {
		// --run mode: execute command and capture output
		cmd := exec.Command(runCmd[0], runCmd[1:]...)
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out
		err := cmd.Run()

		inputData = out.Bytes()

		ec := 0
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				ec = exitErr.ExitCode()
			}
		}
		if ec != 0 {
			exitCode = strconv.Itoa(ec)
			fmt.Fprintf(os.Stderr, "note: command exited with code %d\n", ec)
		}

		// Save argv[1:] as args
		if len(runCmd) > 1 {
			argsValue = strings.Join(runCmd[1:], " ")
		}
	} else if fromFile != "" {
		// --from-file mode
		var err error
		inputData, err = os.ReadFile(fromFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: cannot read file: %v\n", err)
			os.Exit(1)
		}
		argsValue = argsFlag
	} else {
		// stdin mode
		var err error
		inputData, err = io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: cannot read stdin: %v\n", err)
			os.Exit(1)
		}
		argsValue = argsFlag
	}

	// Write input file
	if err := os.WriteFile(inputFile, inputData, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot write input file: %v\n", err)
		os.Exit(1)
	}
	createdFiles = append(createdFiles, inputFile)

	// Write args file if provided
	if argsValue != "" {
		argsFilePath := filepath.Join(caseDir, caseName+".args")
		if err := os.WriteFile(argsFilePath, []byte(argsValue), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "error: cannot write args file: %v\n", err)
			cleanupFiles(createdFiles)
			os.Exit(1)
		}
		createdFiles = append(createdFiles, argsFilePath)
	}

	// Write exit file if non-zero
	if exitCode != "" {
		exitFilePath := filepath.Join(caseDir, caseName+".exit")
		if err := os.WriteFile(exitFilePath, []byte(exitCode), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "error: cannot write exit file: %v\n", err)
			cleanupFiles(createdFiles)
			os.Exit(1)
		}
		createdFiles = append(createdFiles, exitFilePath)
	}

	// Open editor for expected output
	tmpFile, err := os.CreateTemp("", "shimsumm-expected-*.txt")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot create temp file: %v\n", err)
		cleanupFiles(createdFiles)
		os.Exit(1)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	// Pre-populate with input content
	tmpFile.Write(inputData)
	tmpFile.Close()

	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}

	editorCmd := exec.Command("sh", "-c", editor+" "+tmpPath)
	editorCmd.Stdin = os.Stdin
	editorCmd.Stdout = os.Stdout
	editorCmd.Stderr = os.Stderr
	if err := editorCmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: editor failed\n")
		cleanupFiles(createdFiles)
		os.Exit(1)
	}

	// Read editor output
	expectedData, err := os.ReadFile(tmpPath)
	if err != nil || len(strings.TrimSpace(string(expectedData))) == 0 {
		fmt.Fprintf(os.Stderr, "error: expected output is empty, aborting\n")
		cleanupFiles(createdFiles)
		os.Exit(1)
	}

	// Write expected file
	expectedFile := filepath.Join(caseDir, caseName+".expected")
	if err := os.WriteFile(expectedFile, expectedData, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot write expected file: %v\n", err)
		cleanupFiles(createdFiles)
		os.Exit(1)
	}

	inputLines := strings.Count(strings.TrimRight(string(inputData), "\n"), "\n") + 1
	expectedLines := strings.Count(strings.TrimRight(string(expectedData), "\n"), "\n") + 1
	fmt.Printf("Created test: %s/%s (input: %d lines, expected: %d lines)\n", filterName, caseName, inputLines, expectedLines)
}

func cleanupFiles(paths []string) {
	for _, p := range paths {
		os.Remove(p)
	}
}

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

// ---- New Filter ----

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

// ---- Doctor ----

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

// ---- Main ----

func main() {
	rootCmd := &cobra.Command{
		Use:   "shimsumm",
		Short: "Transparent output filtering for LLM-managed shells",
		Long:  "Transparent output filtering for LLM-managed shells",
		// When invoked with no subcommand, print usage and exit 1.
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.Help()
			os.Exit(1)
			return nil
		},
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	rootCmd.SetOut(os.Stdout)

	// ---- init ----
	var dontShim, onlyShim []string
	initCmd := &cobra.Command{
		Use:   "init [bash|zsh|fish|sh]",
		Short: "Print shell setup code",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			shell := "sh"
			if len(args) > 0 {
				shell = args[0]
			}
			validShells := map[string]bool{"bash": true, "zsh": true, "fish": true, "sh": true}
			if !validShells[shell] {
				fmt.Fprintf(os.Stderr, "Usage: shimsumm init [bash|zsh|fish|sh]\n")
				fmt.Fprintf(os.Stderr, "shimsumm: error: invalid choice: '%s' (choose from 'bash', 'zsh', 'fish', 'sh')\n", shell)
				os.Exit(1)
			}
			if len(dontShim) > 0 && len(onlyShim) > 0 {
				fmt.Fprintf(os.Stderr, "shimsumm: error: --dont-shim and --only-shim are mutually exclusive\n")
				os.Exit(1)
			}
			cmdInit(shell, dontShim, onlyShim)
			return nil
		},
	}
	initCmd.Flags().StringSliceVar(&dontShim, "dont-shim", nil, "tool to exclude from shimming (repeatable)")
	initCmd.Flags().StringSliceVar(&onlyShim, "only-shim", nil, "tool to exclusively shim (repeatable)")
	initCmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		msg := err.Error()
		if strings.Contains(msg, "dont-shim") {
			fmt.Fprintf(os.Stderr, "shimsumm: error: --dont-shim requires a tool name\n")
		} else if strings.Contains(msg, "only-shim") {
			fmt.Fprintf(os.Stderr, "shimsumm: error: --only-shim requires a tool name\n")
		} else {
			fmt.Fprintf(os.Stderr, "shimsumm: error: %v\n", err)
		}
		os.Exit(1)
		return nil
	})
	rootCmd.AddCommand(initCmd)

	// ---- emit-wrap ----
	emitWrapCmd := &cobra.Command{
		Use:   "emit-wrap",
		Short: "Print smsm_wrap function definition",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmdWrap()
			return nil
		},
	}
	rootCmd.AddCommand(emitWrapCmd)

	// ---- test ----
	testCmd := &cobra.Command{
		Use:   "test [run|add|list|prompt] ...",
		Short: "Develop and test filter scripts",
		// Handle bare "shimsumm test" and "shimsumm test <filter>"
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				cmdTestRun("")
			} else {
				// Check if first arg is a known filter
				filtersDir := getFiltersDir()
				filterPath := filepath.Join(filtersDir, args[0])
				if stat, err := os.Stat(filterPath); err == nil && (stat.Mode()&0111) != 0 {
					cmdTestRun(args[0])
				} else {
					return fmt.Errorf("unknown command %q for \"shimsumm test\"", args[0])
				}
			}
			return nil
		},
	}
	rootCmd.AddCommand(testCmd)

	testRunCmd := &cobra.Command{
		Use:   "run [<filter>]",
		Short: "Run tests (default when no subcommand given)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			filter := ""
			if len(args) > 0 {
				filter = args[0]
			}
			cmdTestRun(filter)
			return nil
		},
	}
	testCmd.AddCommand(testRunCmd)

	var listAll, listJSON bool
	testListCmd := &cobra.Command{
		Use:   "list [<filter>]",
		Short: "List test cases",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			filter := ""
			if len(args) > 0 {
				filter = args[0]
			}
			cmdTestList(filter, listAll, listJSON)
			return nil
		},
	}
	testListCmd.Flags().BoolVar(&listAll, "all", false, "include filters with no test cases")
	testListCmd.Flags().BoolVar(&listJSON, "json", false, "output structured JSON")
	testCmd.AddCommand(testListCmd)

	var addFromFile, addArgs string
	var addRun bool
	testAddCmd := &cobra.Command{
		Use:                "add <filter> <case> [flags]",
		Short:              "Create a new test case",
		Args:               cobra.ArbitraryArgs,
		DisableFlagParsing: false,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) < 2 {
				return fmt.Errorf("requires at least 2 args: <filter> <case>")
			}
			filterName := args[0]
			caseName := args[1]

			var runCmd []string
			if addRun {
				if addFromFile != "" {
					return fmt.Errorf("--run and --from-file are mutually exclusive")
				}
				if addArgs != "" {
					return fmt.Errorf("--run and --args are mutually exclusive")
				}
				runCmd = args[2:]
				if len(runCmd) == 0 {
					return fmt.Errorf("--run requires a command")
				}
			}

			cmdTestAdd(filterName, caseName, addFromFile, addArgs, runCmd)
			return nil
		},
	}
	testAddCmd.Flags().StringVar(&addFromFile, "from-file", "", "read input from a file instead of stdin")
	testAddCmd.Flags().StringVar(&addArgs, "args", "", "record the command args for this test case")
	testAddCmd.Flags().BoolVar(&addRun, "run", false, "run remaining args as command and capture output")
	testCmd.AddCommand(testAddCmd)

	// ---- dispatch ----
	dispatchCmd := &cobra.Command{
		Use:   "dispatch TOOL [ARGS...]",
		Short: "Dispatch to filter script",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) < 1 {
				fmt.Fprintf(os.Stderr, "Usage: shimsumm dispatch TOOL [ARGS...]\n")
				fmt.Fprintf(os.Stderr, "shimsumm: error: the following arguments are required: tool\n")
				os.Exit(1)
			}
			tool := args[0]
			remainingArgs := args[1:]
			cmdDispatch(tool, remainingArgs)
			return nil
		},
	}
	rootCmd.AddCommand(dispatchCmd)

	// ---- new-filter ----
	newFilterCmd := &cobra.Command{
		Use:   "new-filter COMMAND",
		Short: "Create a passthrough filter for a command",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmdNewFilter(args[0])
			return nil
		},
	}
	rootCmd.AddCommand(newFilterCmd)

	// ---- doctor ----
	var doctorVerbose bool
	doctorCmd := &cobra.Command{
		Use:   "doctor",
		Short: "Validate filter configuration",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmdDoctor(doctorVerbose)
			return nil
		},
	}
	doctorCmd.Flags().BoolVarP(&doctorVerbose, "verbose", "v", false, "show all check results")
	rootCmd.AddCommand(doctorCmd)

	// ---- completion ----
	completionCmd := &cobra.Command{
		Use:   "completion [bash|zsh|fish|powershell]",
		Short: "Generate shell completion script",
		Long: `Generate shell completion script for the specified shell.
To load completions:

Bash:
  $ source <(shimsumm completion bash)

Zsh:
  $ source <(shimsumm completion zsh)

Fish:
  $ shimsumm completion fish | source
`,
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return rootCmd.GenBashCompletionV2(os.Stdout, true)
			case "zsh":
				return rootCmd.GenZshCompletion(os.Stdout)
			case "fish":
				return rootCmd.GenFishCompletion(os.Stdout, true)
			case "powershell":
				return rootCmd.GenPowerShellCompletionWithDesc(os.Stdout)
			default:
				return fmt.Errorf("unsupported shell: %s", args[0])
			}
		},
	}
	rootCmd.AddCommand(completionCmd)

	if err := rootCmd.Execute(); err != nil {
		// Unknown subcommand: cobra prints "unknown command" to stderr.
		// We need to also match the test expectation of exit 1 and output containing
		// the bad subcommand name (cobra includes it in the error message).
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// generateUnifiedDiff creates a unified diff output similar to Python's difflib
func generateUnifiedDiff(fromFile, toFile string, fromLines, toLines []string) string {
	var result strings.Builder
	result.WriteString(fmt.Sprintf("--- %s\n", fromFile))
	result.WriteString(fmt.Sprintf("+++ %s\n", toFile))

	// Generate basic unified diff format with @@ header
	maxLines := len(fromLines)
	if len(toLines) > maxLines {
		maxLines = len(toLines)
	}

	// Create @@ header (simplified: just show line numbers)
	fromLineCount := len(fromLines)
	toLineCount := len(toLines)
	result.WriteString(fmt.Sprintf("@@ -%d +%d @@\n", 1, 1))

	// Output diff lines
	for i := 0; i < maxLines; i++ {
		if i < len(fromLines) && i < len(toLines) {
			if fromLines[i] != toLines[i] {
				result.WriteString(fmt.Sprintf("-%s\n", fromLines[i]))
				result.WriteString(fmt.Sprintf("+%s\n", toLines[i]))
			} else {
				result.WriteString(fmt.Sprintf(" %s\n", fromLines[i]))
			}
		} else if i < len(fromLines) {
			result.WriteString(fmt.Sprintf("-%s\n", fromLines[i]))
		} else {
			result.WriteString(fmt.Sprintf("+%s\n", toLines[i]))
		}
	}

	_ = fromLineCount // For later enhancement if needed
	_ = toLineCount
	return result.String()
}
