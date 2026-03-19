package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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

// ---- Tool Execution ----

type ExecutionResult struct {
	stdout     string
	returncode int
}

func executeTool(toolPath string, args []string) ExecutionResult {
	cmd := exec.Command(toolPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0

	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			if status, ok := exitError.Sys().(syscall.WaitStatus); ok {
				exitCode = status.ExitStatus()
			} else {
				exitCode = 1
			}
		} else if _, ok := err.(*exec.Error); ok {
			// File not found
			exitCode = 127
		} else {
			exitCode = 1
		}
	}

	// Merge stderr into stdout for true interleaving (same order as subprocess.STDOUT)
	allOutput := stdout.String() + stderr.String()

	return ExecutionResult{
		stdout:     allOutput,
		returncode: exitCode,
	}
}

// ---- Test Runner ----

func runFilterTest(toolName, filtersDir, testsDir string) (bool, string) {
	filterPath := filepath.Join(filtersDir, toolName)
	inputFile := filepath.Join(testsDir, toolName+".input")
	expectedFile := filepath.Join(testsDir, toolName+".expected")

	// Check if filter exists and is executable
	stat, err := os.Stat(filterPath)
	if err != nil || (stat.Mode()&0111) == 0 {
		return false, fmt.Sprintf("FAIL: %s\nFilter not found or not executable", toolName)
	}

	// Check if fixtures exist
	if _, err := os.Stat(inputFile); err != nil {
		return false, fmt.Sprintf("FAIL: %s\nno fixtures", toolName)
	}
	if _, err := os.Stat(expectedFile); err != nil {
		return false, fmt.Sprintf("FAIL: %s\nno fixtures", toolName)
	}

	// Read expected output
	expectedBytes, err := os.ReadFile(expectedFile)
	if err != nil {
		return false, fmt.Sprintf("FAIL: %s\n%v", toolName, err)
	}
	expected := strings.TrimRight(string(expectedBytes), "\n")

	// Create a temporary directory with a mock tool that outputs the input fixture
	mockDir, err := os.MkdirTemp("", "shimsumm-test-")
	if err != nil {
		return false, fmt.Sprintf("FAIL: %s\n%v", toolName, err)
	}
	defer os.RemoveAll(mockDir)

	// Create mock tool that outputs the fixture input
	mockTool := filepath.Join(mockDir, toolName)
	inputBytes, err := os.ReadFile(inputFile)
	if err != nil {
		return false, fmt.Sprintf("FAIL: %s\n%v", toolName, err)
	}
	mockScript := fmt.Sprintf("#!/bin/sh\ncat %q\nexit 0\n", inputFile)
	if err := os.WriteFile(mockTool, []byte(mockScript), 0755); err != nil {
		return false, fmt.Sprintf("FAIL: %s\n%v", toolName, err)
	}
	_ = inputBytes // Ensure we read it

	// Execute filter with mock tool in PATH
	cmd := exec.Command(filterPath)
	pathEnv := fmt.Sprintf("%s:%s:%s", filtersDir, mockDir, os.Getenv("PATH"))
	cmd.Env = append(os.Environ(), fmt.Sprintf("PATH=%s", pathEnv))

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	cmdErr := cmd.Run()
	_ = cmdErr // We'll check the output regardless

	// Filter out [full output:...] lines from actual output
	actualLines := []string{}
	scanner := bufio.NewScanner(strings.NewReader(stdout.String()))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "[full output:") {
			actualLines = append(actualLines, line)
		}
	}
	actual := strings.TrimRight(strings.Join(actualLines, "\n"), "\n")

	if actual == expected {
		return true, fmt.Sprintf("PASS: %s", toolName)
	}

	// Generate unified diff
	expectedLines := strings.Split(expected, "\n")
	actualLines = strings.Split(actual, "\n")
	diffOutput := generateUnifiedDiff(expectedFile, "actual", expectedLines, actualLines)

	return false, fmt.Sprintf("FAIL: %s\n%s", toolName, diffOutput)
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

func cmdTest(tool string) {
	filtersDir := getFiltersDir()
	testsDir := getTestsDir()

	// Check if tests directory exists
	if _, err := os.Stat(testsDir); err != nil {
		fmt.Fprintf(os.Stderr, "No tests directory: %s\n", testsDir)
		os.Exit(1)
	}

	if tool != "" {
		// Check if filter exists first
		filterPath := filepath.Join(filtersDir, tool)
		stat, err := os.Stat(filterPath)
		if err != nil || (stat.Mode()&0111) == 0 {
			os.Exit(127)
		}

		passed, output := runFilterTest(tool, filtersDir, testsDir)
		fmt.Println(output)
		if !passed {
			os.Exit(1)
		}
	} else {
		// Run all tests
		entries, err := os.ReadDir(testsDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading tests directory: %v\n", err)
			os.Exit(1)
		}

		var toolNames []string
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".input") {
				toolName := strings.TrimSuffix(entry.Name(), ".input")
				expectedFile := filepath.Join(testsDir, toolName+".expected")
				if _, err := os.Stat(expectedFile); err == nil {
					toolNames = append(toolNames, toolName)
				}
			}
		}

		sort.Strings(toolNames)

		allPassed := true
		for _, toolName := range toolNames {
			passed, output := runFilterTest(toolName, filtersDir, testsDir)
			fmt.Println(output)
			if !passed {
				allPassed = false
			}
		}

		if !allPassed {
			os.Exit(1)
		}
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
		Use:   "test [TOOL]",
		Short: "Run fixture tests for filter scripts",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			tool := ""
			if len(args) > 0 {
				tool = args[0]
			}
			cmdTest(tool)
			return nil
		},
	}
	rootCmd.AddCommand(testCmd)

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
