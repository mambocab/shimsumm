package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var version = "dev"

func cmdInit(shell string, dontShim, onlyShim []string) {
	filtersDir := getFiltersDir()

	var code string
	if shell == "fish" {
		code = fmt.Sprintf(`set -l _smsm_f "%s"
contains -- $_smsm_f $PATH; or set -gx PATH $_smsm_f $PATH
set -e _smsm_f`, filtersDir)
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
	rootCmd.Version = version
	rootCmd.SetOut(os.Stdout)
	rootCmd.AddGroup(&cobra.Group{ID: "user", Title: "Commands:"})
	rootCmd.AddGroup(&cobra.Group{ID: "internal", Title: "Internal Commands:"})

	// ---- init ----
	var dontShim, onlyShim []string
	initCmd := &cobra.Command{
		GroupID: "user",
		Use:     "init [bash|zsh|fish|sh]",
		Short:   "Print shell setup code",
		Args:    cobra.MaximumNArgs(1),
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
		GroupID: "internal",
		Use:     "emit-wrap",
		Short:   "Print smsm_wrap function definition",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmdWrap()
			return nil
		},
	}
	rootCmd.AddCommand(emitWrapCmd)

	// ---- test ----
	testCmd := &cobra.Command{
		GroupID: "user",
		Use:     "test [command]",
		Short:   "Develop and test filter scripts",
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
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmdTestRun("")
			return nil
		},
	}
	rootCmd.AddCommand(testCmd)

	testRunCmd := &cobra.Command{
		Use:               "run [<filter>]",
		Short:             "Run tests (default when no subcommand given)",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeFilterNames,
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
		Use:               "list [<filter>]",
		Short:             "List test cases",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeFilterNames,
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
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			if len(args) == 0 {
				return completeFilterNames(cmd, args, toComplete)
			}
			return nil, cobra.ShellCompDirectiveNoFileComp
		},
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

	testPromptCmd := &cobra.Command{
		Use:               "prompt <filter>",
		Short:             "Generate a prompt for LLM-assisted filter development",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeFilterNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmdTestPrompt(args[0])
			return nil
		},
	}
	testCmd.AddCommand(testPromptCmd)

	// ---- dispatch ----
	dispatchCmd := &cobra.Command{
		GroupID: "internal",
		Use:     "dispatch TOOL [ARGS...]",
		Short:   "Dispatch to filter script",
		Args:    cobra.ArbitraryArgs,
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			if len(args) == 0 {
				return completeFilterNames(cmd, args, toComplete)
			}
			return nil, cobra.ShellCompDirectiveDefault
		},
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
		GroupID: "user",
		Use:     "new-filter COMMAND",
		Short:   "Create a passthrough filter for a command",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmdNewFilter(args[0])
			return nil
		},
	}
	rootCmd.AddCommand(newFilterCmd)

	// ---- doctor ----
	var doctorVerbose bool
	doctorCmd := &cobra.Command{
		GroupID: "user",
		Use:     "doctor",
		Short:   "Validate filter configuration",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmdDoctor(doctorVerbose)
			return nil
		},
	}
	doctorCmd.Flags().BoolVarP(&doctorVerbose, "verbose", "v", false, "show all check results")
	rootCmd.AddCommand(doctorCmd)

	// ---- completion ----
	completionCmd := &cobra.Command{
		GroupID: "internal",
		Use:     "completion [bash|zsh|fish|powershell]",
		Short:   "Generate shell completion script",
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
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
