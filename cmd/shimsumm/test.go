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

	"github.com/pmezard/go-difflib/difflib"
)

type testCase struct {
	filter string
	name   string
}

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
	var env []string
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "PATH=") {
			env = append(env, e)
		}
	}
	env = append(env, fmt.Sprintf("PATH=%s", pathEnv))
	cmd.Env = env

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

	// Generate unified diff
	diff := difflib.UnifiedDiff{
		A:        difflib.SplitLines(expected),
		B:        difflib.SplitLines(actual),
		FromFile: expectedFile,
		ToFile:   "actual",
		Context:  3,
	}
	diffOutput, _ := difflib.GetUnifiedDiffString(diff)

	var result strings.Builder
	fmt.Fprintf(&result, "FAIL: %s\n", label)
	if !outputMatch {
		result.WriteString(diffOutput)
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

// confirmPrompt prints a question and reads a y/n response from stdin.
// defaultYes controls what happens when the user presses enter with no input:
// true means default is Y (shown as [Y/n]), false means default is N (shown as [y/N]).
func confirmPrompt(question string, defaultYes bool) bool {
	hint := "[y/N]"
	if defaultYes {
		hint = "[Y/n]"
	}
	fmt.Fprintf(os.Stderr, "%s %s ", question, hint)

	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	answer := strings.TrimSpace(strings.ToLower(line))

	if answer == "" {
		return defaultYes
	}
	return answer == "y" || answer == "yes"
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

	// Check if a filter exists for this name; if not, offer to create one
	filtersDir := getFiltersDir()
	filterPath := filepath.Join(filtersDir, filterName)
	if _, err := os.Stat(filterPath); err != nil {
		if !confirmPrompt(fmt.Sprintf("No filter %q exists. Create it?", filterName), true) {
			fmt.Fprintf(os.Stderr, "aborted\n")
			os.Exit(1)
		}
		cmdNewFilter(filterName)
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
		// --run mode: check if the command name matches the filter name
		cmdBaseName := filepath.Base(runCmd[0])
		if cmdBaseName != filterName {
			if !confirmPrompt(fmt.Sprintf("Command %q doesn't match filter %q. Continue?", cmdBaseName, filterName), false) {
				fmt.Fprintf(os.Stderr, "aborted\n")
				os.Exit(1)
			}
		}

		// Execute command and capture output
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

	// Confirm before opening the editor. Dropping users straight into an
	// editor without warning is disorienting, so we always ask first.
	if !confirmPrompt("Open editor to define expected output?", true) {
		fmt.Fprintf(os.Stderr, "aborted\n")
		cleanupFiles(createdFiles)
		os.Exit(1)
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

func cmdTestPrompt(filterName string) {
	filtersDir := getFiltersDir()
	filterPath := filepath.Join(filtersDir, filterName)

	filterExists := false
	if stat, err := os.Stat(filterPath); err == nil && (stat.Mode()&0111) != 0 {
		filterExists = true
	}

	var b strings.Builder

	b.WriteString("# Task: Write a shimsumm filter for " + filterName + "\n\n")

	b.WriteString("## What is shimsumm?\n\n")
	b.WriteString("shimsumm is a tool that interposes filter scripts between command-line tools and the user. ")
	b.WriteString("When a shimmed command runs, shimsumm captures its output and pipes it through a filter script ")
	b.WriteString("that can transform, summarize, or annotate the output.\n\n")

	b.WriteString("## How filter scripts work\n\n")
	b.WriteString("A filter script lives at: `" + filterPath + "`\n\n")
	b.WriteString("The script sources the `smsm_wrap` function, defines a `smsm_filter()` function, ")
	b.WriteString("and calls `smsm_wrap \"$@\"`. The `smsm_filter()` function reads the raw tool output ")
	b.WriteString("from stdin and writes the filtered output to stdout.\n\n")

	if filterExists {
		b.WriteString("**An existing filter is already at this path. Modify it rather than starting from scratch.**\n\n")
	}

	b.WriteString("## Filter script skeleton\n\n")
	b.WriteString("```sh\n")
	b.WriteString("#!/bin/sh\n")
	b.WriteString("eval \"$(shimsumm emit-wrap)\"\n\n")
	b.WriteString("smsm_filter() {\n")
	b.WriteString("  # Read raw tool output from stdin\n")
	b.WriteString("  # Write filtered output to stdout\n")
	b.WriteString("  cat  # passthrough — replace with your filter logic\n")
	b.WriteString("}\n\n")
	b.WriteString("smsm_wrap \"$@\"\n")
	b.WriteString("```\n\n")

	b.WriteString("## Running tests\n\n")
	b.WriteString("Run: `shimsumm test run " + filterName + "`\n\n")
	b.WriteString("Test output shows unified diffs when the filter's actual output doesn't match expected output. ")
	b.WriteString("Lines prefixed with `-` are expected but missing; lines prefixed with `+` are unexpected. ")
	b.WriteString("Exit code mismatches are reported separately.\n\n")

	b.WriteString("## Development loop\n\n")
	b.WriteString("1. Edit the filter script at `" + filterPath + "`\n")
	b.WriteString("2. Run `shimsumm test run " + filterName + "`\n")
	b.WriteString("3. Read the test failures (diffs and exit code mismatches)\n")
	b.WriteString("4. Repeat until all tests pass\n")

	fmt.Print(b.String())
}
