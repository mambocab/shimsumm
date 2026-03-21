# Test Subcommand Rework Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Redesign `shimsumm test` from a flat test runner into a multi-subcommand tool supporting directory-based fixtures, test case creation, listing, and LLM prompt generation.

**Architecture:** Replace the current `test [TOOL]` cobra command with a `test` parent command and four subcommands (`run`, `add`, `list`, `prompt`). The test runner switches from flat `<tool>.input`/`<tool>.expected` files to directory-based `<filter>/<case>.{input,expected,args,exit}` fixtures. Bare `shimsumm test` and `shimsumm test <filter>` (when matching an existing filter) route to `test run`.

**Tech Stack:** Go 1.26, Cobra v1.10.2, BATS for integration tests

**Spec:** `docs/superpowers/specs/2026-03-20-test-subcommand-design.md`

---

## File Structure

All Go implementation is in `cmd/shimsumm/main.go` (single-file CLI, following existing pattern). Test changes go in `tests/shimsumm-test.bats`.

- **Modify:** `cmd/shimsumm/main.go` — replace `cmdTest` and `runFilterTest` with new subcommand handlers and updated test runner
- **Modify:** `tests/shimsumm-test.bats` — rewrite tests for the new directory-based format and new subcommands

---

## Task 1: Rewrite test discovery and runner for directory-based fixtures

Replaces the flat-file test discovery (`<tool>.input`/`<tool>.expected`) with directory-based discovery (`<filter>/<case>.input`/`<case>.expected`), and adds exit code and args support to the test runner.

**Files:**
- Modify: `cmd/shimsumm/main.go:152-332` (replace `runFilterTest` and `cmdTest`)
- Modify: `tests/shimsumm-test.bats` (rewrite for new fixture layout)

### Concepts

**Test case discovery:** Scan `tests/<filter>/` directories. Within each, find `*.input` files. Each `.input` with a matching `.expected` is a valid test case. A `.input` without `.expected` triggers a warning to stderr.

**Exit code handling:** Read `<case>.exit` file. If absent, mock exits 0 and expect exit 0. If contains a number, mock exits with that number and expect that exact code. If contains `nonzero`, mock exits 1 and expect any non-zero code.

**Args handling:** Read `<case>.args` file. Pass its contents as arguments to the filter script invocation.

**Output format:** `PASS: <filter>/<case>` or `FAIL: <filter>/<case>` followed by diff and/or exit code mismatch details.

- [ ] **Step 1: Rewrite BATS tests for directory-based fixtures**

Replace the contents of `tests/shimsumm-test.bats` with tests that use the new `tests/<filter>/<case>.{input,expected}` layout. The setup creates `$TESTS_DIR/mytool/` instead of flat files.

```bash
#!/usr/bin/env bats
load 'test_helper'

setup() {
  TEST_TMP=$(mktemp -d)
  export XDG_CONFIG_HOME="$TEST_TMP/config"
  FILTERS_DIR="$XDG_CONFIG_HOME/shimsumm/filters"
  TESTS_DIR="$XDG_CONFIG_HOME/shimsumm/tests"
  mkdir -p "$FILTERS_DIR" "$TESTS_DIR/mytool"

  # Filter script: keeps only lines matching KEEP
  cat > "$FILTERS_DIR/mytool" <<'FILTEREOF'
#!/bin/sh
eval "$(shimsumm emit-wrap)"
smsm_filter() { grep KEEP || true; }
smsm_wrap "$@"
FILTEREOF
  chmod +x "$FILTERS_DIR/mytool"

  # Directory-based fixtures
  printf 'KEEP this line\nDROP this line\n' > "$TESTS_DIR/mytool/basic.input"
  printf 'KEEP this line\n' > "$TESTS_DIR/mytool/basic.expected"

  export PATH="$FILTERS_DIR:$PROJECT_ROOT/bin:$PATH"
}

teardown() {
  rm -rf "$TEST_TMP"
}

@test "test run: exits 0 when output matches expected" {
  run shimsumm test run mytool
  assert_success
  assert_output --partial "PASS: mytool/basic"
}

@test "test run: exits 1 when output does not match expected" {
  printf 'WRONG expected output\n' > "$TESTS_DIR/mytool/basic.expected"
  run shimsumm test run mytool
  assert_failure
  assert_output --partial "FAIL: mytool/basic"
}

@test "test run: shows diff on mismatch" {
  printf 'WRONG\n' > "$TESTS_DIR/mytool/basic.expected"
  run shimsumm test run mytool
  assert_output --partial "KEEP"
  assert_output --partial "WRONG"
}

@test "test run: multiple cases in one filter" {
  printf 'KEEP second\nDROP second\n' > "$TESTS_DIR/mytool/second.input"
  printf 'KEEP second\n' > "$TESTS_DIR/mytool/second.expected"
  run shimsumm test run mytool
  assert_success
  assert_output --partial "PASS: mytool/basic"
  assert_output --partial "PASS: mytool/second"
}

@test "test run: warns on .input without .expected" {
  printf 'orphan input\n' > "$TESTS_DIR/mytool/orphan.input"
  run shimsumm test run mytool
  assert_success
  assert_output --partial "PASS: mytool/basic"
  # Warning goes to stderr — check combined
  assert_output --partial "warning"
}

@test "test run: exit code from .exit file (numeric)" {
  mkdir -p "$TESTS_DIR/mytool2"
  cat > "$FILTERS_DIR/mytool2" <<'FILTEREOF'
#!/bin/sh
eval "$(shimsumm emit-wrap)"
smsm_wrap "$@"
FILTEREOF
  chmod +x "$FILTERS_DIR/mytool2"
  printf 'some output\n' > "$TESTS_DIR/mytool2/failing.input"
  printf 'some output\n' > "$TESTS_DIR/mytool2/failing.expected"
  printf '1' > "$TESTS_DIR/mytool2/failing.exit"
  run shimsumm test run mytool2
  assert_success
}

@test "test run: exit code mismatch fails" {
  mkdir -p "$TESTS_DIR/mytool2"
  cat > "$FILTERS_DIR/mytool2" <<'FILTEREOF'
#!/bin/sh
eval "$(shimsumm emit-wrap)"
smsm_wrap "$@"
FILTEREOF
  chmod +x "$FILTERS_DIR/mytool2"
  printf 'some output\n' > "$TESTS_DIR/mytool2/failing.input"
  printf 'some output\n' > "$TESTS_DIR/mytool2/failing.expected"
  printf '2' > "$TESTS_DIR/mytool2/failing.exit"
  run shimsumm test run mytool2
  assert_failure
  assert_output --partial "exit code"
}

@test "test run: nonzero exit code" {
  mkdir -p "$TESTS_DIR/mytool2"
  cat > "$FILTERS_DIR/mytool2" <<'FILTEREOF'
#!/bin/sh
eval "$(shimsumm emit-wrap)"
smsm_wrap "$@"
FILTEREOF
  chmod +x "$FILTERS_DIR/mytool2"
  printf 'some output\n' > "$TESTS_DIR/mytool2/case1.input"
  printf 'some output\n' > "$TESTS_DIR/mytool2/case1.expected"
  printf 'nonzero' > "$TESTS_DIR/mytool2/case1.exit"
  run shimsumm test run mytool2
  assert_success
}

@test "test run: args from .args file" {
  printf 'KEEP this line\nDROP this line\n' > "$TESTS_DIR/mytool/withargs.input"
  printf 'KEEP this line\n' > "$TESTS_DIR/mytool/withargs.expected"
  printf -- '-v --flag' > "$TESTS_DIR/mytool/withargs.args"
  run shimsumm test run mytool
  assert_success
  assert_output --partial "PASS: mytool/withargs"
}

@test "test run: no args runs all filters" {
  mkdir -p "$TESTS_DIR/othertool"
  cat > "$FILTERS_DIR/othertool" <<'FILTEREOF'
#!/bin/sh
eval "$(shimsumm emit-wrap)"
smsm_wrap "$@"
FILTEREOF
  chmod +x "$FILTERS_DIR/othertool"
  printf 'some input\n' > "$TESTS_DIR/othertool/case1.input"
  printf 'some input\n' > "$TESTS_DIR/othertool/case1.expected"
  run shimsumm test run
  assert_success
  assert_output --partial "PASS: mytool/basic"
  assert_output --partial "PASS: othertool/case1"
}

@test "bare 'shimsumm test' runs all tests" {
  run shimsumm test
  assert_success
  assert_output --partial "PASS: mytool/basic"
}

@test "'shimsumm test <filter>' routes to test run" {
  run shimsumm test mytool
  assert_success
  assert_output --partial "PASS: mytool/basic"
}
```

- [ ] **Step 2: Build and run tests to verify they fail**

Run: `just test`
Expected: Tests fail because the Go implementation still uses the flat format.

- [ ] **Step 3: Rewrite runFilterTest for directory-based fixtures**

Replace `runFilterTest` (lines 152-230) with a new version that:
- Takes `filterName`, `caseName`, `filtersDir`, `testsDir` parameters
- Reads from `tests/<filter>/<case>.input` and `tests/<filter>/<case>.expected`
- Reads `<case>.exit` for exit code expectations
- Reads `<case>.args` for command arguments
- Creates mock binary that exits with the code from `.exit`
- Passes args from `.args` to the filter invocation
- Compares both output and exit code
- Returns pass/fail with `<filter>/<case>` in labels

```go
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
```

- [ ] **Step 4: Rewrite discoverTestCases and cmdTestRun**

Replace `cmdTest` (lines 275-332) with:
- `discoverTestCases(testsDir, filterName string)` — returns list of `{filter, case}` pairs
- `cmdTestRun(filterName string)` — runs discovered test cases, prints results, exits 0/1

```go
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
```

- [ ] **Step 5: Wire up the Cobra `test` parent command and `test run` subcommand**

Replace the `test` command registration (lines 704-718) with a parent `test` command that has `run` as a subcommand. The parent command handles bare `shimsumm test` and `shimsumm test <filter>` routing.

```go
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
```

- [ ] **Step 6: Add `"strconv"` to imports**

Add `"strconv"` to the import block at the top of `main.go` (line 3-15) since `runFilterTest` now uses `strconv.Atoi`.

- [ ] **Step 7: Build and run tests**

Run: `just test`
Expected: All tests in `shimsumm-test.bats` pass. Other test files should still pass unchanged.

- [ ] **Step 8: Commit**

```bash
git add cmd/shimsumm/main.go tests/shimsumm-test.bats
git commit -m "feat: rewrite test runner for directory-based fixtures

Replaces flat <tool>.input/<tool>.expected format with
<filter>/<case>.{input,expected,args,exit} directories.
Adds exit code validation and args file support.
Wires up 'test run' subcommand with bare 'test' routing."
```

---

## Task 2: Implement `test list`

**Files:**
- Modify: `cmd/shimsumm/main.go` — add `cmdTestList` function and wire up subcommand
- Modify: `tests/shimsumm-test.bats` — add list tests

- [ ] **Step 1: Add BATS tests for `test list`**

Append these tests to `tests/shimsumm-test.bats`:

```bash
@test "test list: shows filters with cases" {
  run shimsumm test list
  assert_success
  assert_output --partial "mytool"
  assert_output --partial "basic"
}

@test "test list: shows annotations for args and exit" {
  printf -- '-v' > "$TESTS_DIR/mytool/basic.args"
  printf '1' > "$TESTS_DIR/mytool/basic.exit"
  run shimsumm test list
  assert_success
  assert_output --partial "(args, exit: 1)"
}

@test "test list: filter argument limits output" {
  mkdir -p "$TESTS_DIR/othertool"
  printf 'x\n' > "$TESTS_DIR/othertool/case1.input"
  printf 'x\n' > "$TESTS_DIR/othertool/case1.expected"
  run shimsumm test list mytool
  assert_success
  assert_output --partial "mytool"
  refute_output --partial "othertool"
}

@test "test list --all: includes filters with no tests" {
  cat > "$FILTERS_DIR/notested" <<'FILTEREOF'
#!/bin/sh
eval "$(shimsumm emit-wrap)"
smsm_wrap "$@"
FILTEREOF
  chmod +x "$FILTERS_DIR/notested"
  run shimsumm test list --all
  assert_success
  assert_output --partial "notested (no tests)"
}

@test "test list --json: emits JSON" {
  run shimsumm test list --json
  assert_success
  # Should be valid JSON with filter and cases
  echo "$output" | python3 -c "import sys,json; d=json.load(sys.stdin); assert any(f['filter']=='mytool' for f in d)"
}

@test "test list --json: includes exit field when .exit exists" {
  printf '1' > "$TESTS_DIR/mytool/basic.exit"
  run shimsumm test list --json
  assert_success
  echo "$output" | python3 -c "
import sys,json
d=json.load(sys.stdin)
mytool = [f for f in d if f['filter']=='mytool'][0]
case = [c for c in mytool['cases'] if c['name']=='basic'][0]
assert case['exit'] == '1', f'got {case}'
"
}

@test "test list --json: includes untested filters" {
  cat > "$FILTERS_DIR/notested" <<'FILTEREOF'
#!/bin/sh
eval "$(shimsumm emit-wrap)"
smsm_wrap "$@"
FILTEREOF
  chmod +x "$FILTERS_DIR/notested"
  run shimsumm test list --json
  assert_success
  echo "$output" | python3 -c "
import sys,json
d=json.load(sys.stdin)
notested = [f for f in d if f['filter']=='notested'][0]
assert notested['cases'] == [], f'got {notested}'
"
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `just test`
Expected: New list tests fail.

- [ ] **Step 3: Implement `cmdTestList`**

Add this function to `main.go`:

```go
func cmdTestList(filterName string, showAll bool, jsonOutput bool) {
	filtersDir := getFiltersDir()
	testsDir := getTestsDir()

	// Discover test cases
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

	// Get filters that have test directories
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
```

- [ ] **Step 4: Add `"encoding/json"` to imports**

Add `"encoding/json"` to the import block.

- [ ] **Step 5: Wire up the `test list` subcommand**

Add after the `testRunCmd` registration:

```go
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
```

- [ ] **Step 6: Build and run tests**

Run: `just test`
Expected: All list tests pass.

- [ ] **Step 7: Commit**

```bash
git add cmd/shimsumm/main.go tests/shimsumm-test.bats
git commit -m "feat: add 'test list' subcommand with --all and --json flags"
```

---

## Task 3: Implement `test add`

**Files:**
- Modify: `cmd/shimsumm/main.go` — add `cmdTestAdd` function and wire up subcommand
- Modify: `tests/shimsumm-test.bats` — add tests for add subcommand

- [ ] **Step 1: Add BATS tests for `test add`**

Append to `tests/shimsumm-test.bats`. Note: testing the editor step requires `$EDITOR` set to a non-interactive command. We use `cp` of a pre-prepared file.

Note: BATS `run` executes in a subshell, so `printf | run cmd` does not deliver stdin to the command. All stdin tests use `--from-file` instead; stdin behavior is identical code path minus the file read. The `--run` flag tests use a helper script.

```bash
@test "test add: creates test case from --from-file" {
  printf 'raw input data\n' > "$TEST_TMP/source.txt"
  export EDITOR="cp $TEST_TMP/expected_content"
  printf 'expected output\n' > "$TEST_TMP/expected_content"
  run shimsumm test add mytool newcase --from-file "$TEST_TMP/source.txt"
  assert_success
  assert_output --partial "Created test: mytool/newcase"
  [ -f "$TESTS_DIR/mytool/newcase.input" ]
  [ -f "$TESTS_DIR/mytool/newcase.expected" ]
  [ "$(cat "$TESTS_DIR/mytool/newcase.input")" = "raw input data" ]
}

@test "test add: saves .args when --args provided" {
  printf 'input data\n' > "$TEST_TMP/source.txt"
  export EDITOR="cp $TEST_TMP/expected_content"
  printf 'expected\n' > "$TEST_TMP/expected_content"
  run shimsumm test add mytool argscase --from-file "$TEST_TMP/source.txt" --args "-v --flag"
  assert_success
  [ -f "$TESTS_DIR/mytool/argscase.args" ]
  [ "$(cat "$TESTS_DIR/mytool/argscase.args")" = "-v --flag" ]
}

@test "test add: errors if case already exists" {
  run shimsumm test add mytool basic
  assert_failure
  assert_output --partial "already exists"
}

@test "test add: creates filter directory if needed" {
  printf 'input\n' > "$TEST_TMP/source.txt"
  export EDITOR="cp $TEST_TMP/expected_content"
  printf 'expected\n' > "$TEST_TMP/expected_content"
  run shimsumm test add brandnew case1 --from-file "$TEST_TMP/source.txt"
  assert_success
  [ -d "$TESTS_DIR/brandnew" ]
}

@test "test add --run: captures command output and exit code" {
  export EDITOR="cp $TEST_TMP/expected_content"
  printf 'expected\n' > "$TEST_TMP/expected_content"
  # Create a script that outputs something and exits 1
  cat > "$TEST_TMP/failing-cmd" <<'EOF'
#!/bin/sh
echo "command output"
exit 1
EOF
  chmod +x "$TEST_TMP/failing-cmd"
  run shimsumm test add mytool runcmd --run "$TEST_TMP/failing-cmd"
  assert_success
  [ "$(cat "$TESTS_DIR/mytool/runcmd.input")" = "command output" ]
  [ "$(cat "$TESTS_DIR/mytool/runcmd.exit")" = "1" ]
}

@test "test add: aborts on empty editor output" {
  printf 'input\n' > "$TEST_TMP/source.txt"
  export EDITOR="cp /dev/null"
  run shimsumm test add mytool emptycase --from-file "$TEST_TMP/source.txt"
  assert_failure
  [ ! -f "$TESTS_DIR/mytool/emptycase.input" ]
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `just test`
Expected: New add tests fail.

- [ ] **Step 3: Implement `cmdTestAdd`**

Add to `main.go`:

```go
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

	editorCmd := exec.Command(editor, tmpPath)
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
```

- [ ] **Step 4: Add `"io"` to imports**

Add `"io"` to the import block.

- [ ] **Step 5: Wire up the `test add` subcommand**

Add after `testListCmd` registration:

```go
var addFromFile, addArgs string
var addRun bool
testAddCmd := &cobra.Command{
	Use:   "add <filter> <case> [flags]",
	Short: "Create a new test case",
	// Use ArbitraryArgs because --run consumes all remaining args
	Args: cobra.ArbitraryArgs,
	// Disable flag parsing after first non-flag so --run's command flags
	// (like -v) aren't consumed by cobra
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
			// Everything after <filter> <case> is the command to run.
			// But cobra has already parsed flags out of args, so remaining
			// args after filter+case are the run command.
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
```

**Important:** `--run` is a boolean flag. When set, all positional args after `<filter> <case>` are treated as the command to execute. This avoids cobra trying to parse the command's flags (like `-v`). However, cobra's default flag parsing may still consume flags that look like shimsumm flags. If this causes issues, the implementer should set `TraverseChildren: false` on the test command or use `cmd.Flags().SetInterspersed(false)` on the add command to stop flag parsing after the first positional arg.

- [ ] **Step 6: Build and run tests**

Run: `just test`
Expected: All add tests pass.

- [ ] **Step 7: Commit**

```bash
git add cmd/shimsumm/main.go tests/shimsumm-test.bats
git commit -m "feat: add 'test add' subcommand for creating test cases"
```

---

## Task 4: Implement `test prompt`

**Files:**
- Modify: `cmd/shimsumm/main.go` — add `cmdTestPrompt` function and wire up subcommand
- Modify: `tests/shimsumm-test.bats` — add prompt tests

- [ ] **Step 1: Add BATS tests for `test prompt`**

Append to `tests/shimsumm-test.bats`:

```bash
@test "test prompt: emits prompt with filter name" {
  run shimsumm test prompt mytool
  assert_success
  assert_output --partial "mytool"
  assert_output --partial "smsm_filter"
  assert_output --partial "smsm_wrap"
  assert_output --partial "shimsumm test run mytool"
}

@test "test prompt: includes filter path" {
  run shimsumm test prompt mytool
  assert_success
  assert_output --partial "filters/mytool"
}

@test "test prompt: mentions existing filter when present" {
  run shimsumm test prompt mytool
  assert_success
  assert_output --partial "modify"
}

@test "test prompt: includes skeleton when no filter exists" {
  run shimsumm test prompt newfilter
  assert_success
  assert_output --partial "smsm_filter"
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `just test`
Expected: New prompt tests fail.

- [ ] **Step 3: Implement `cmdTestPrompt`**

Add to `main.go`:

```go
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
```

- [ ] **Step 4: Wire up the `test prompt` subcommand**

Add after `testAddCmd` registration:

```go
testPromptCmd := &cobra.Command{
	Use:   "prompt <filter>",
	Short: "Generate a prompt for LLM-assisted filter development",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cmdTestPrompt(args[0])
		return nil
	},
}
testCmd.AddCommand(testPromptCmd)
```

- [ ] **Step 5: Build and run tests**

Run: `just test`
Expected: All tests pass.

- [ ] **Step 6: Commit**

```bash
git add cmd/shimsumm/main.go tests/shimsumm-test.bats
git commit -m "feat: add 'test prompt' subcommand for LLM prompt generation"
```

---

## Task 5: Add help text and final polish

**Files:**
- Modify: `cmd/shimsumm/main.go` — add Long description to test command

- [ ] **Step 1: Add the help text from the spec**

Set the `Long` field on the `testCmd` cobra command:

```go
Long: `Develop and test filter scripts.

Subcommands:
  run [<filter>]       Run tests (default when no subcommand given)
  add <filter> <name>  Create a new test case
  list [<filter>]      List test cases
  prompt <filter>      Generate a prompt for LLM-assisted filter development

Flags (for list):
  --all                Include filters with no test cases
  --json               Output structured JSON

Flags (for add):
  --from-file <path>   Read input from a file instead of stdin
  --run <command...>   Run a command and capture its output
  --args "..."         Record the command args for this test case

Workflow:
  1. Capture interesting examples of tool output:
       some-command | shimsumm test add myfilter case1
       shimsumm test add myfilter case2 --run some-command --flag

  2. For each, an editor opens to define the expected output.

  3. Generate a prompt for your coding agent:
       shimsumm test prompt myfilter | pbcopy

  4. Give the prompt to your LLM coding tool. It will edit the
     filter script and run "shimsumm test run myfilter" in a loop
     until the tests pass.`,
```

- [ ] **Step 2: Build and run all tests**

Run: `just test`
Expected: All tests pass across all BATS files.

- [ ] **Step 3: Commit**

```bash
git add cmd/shimsumm/main.go
git commit -m "docs: add help text and workflow description to test command"
```
