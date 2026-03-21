# Test Subcommand Design Spec

Date: 2026-03-20

## Purpose

Redesign `shimsumm test` to support a complete filter development workflow: capture example tool output, define expected filtered results, run tests, and generate prompts for LLM coding agents to write the filter logic.

## Test Directory Structure

Test fixtures live in `~/.config/shimsumm/tests/<filter>/`, with one set of files per named test case:

```
~/.config/shimsumm/tests/
  <filter>/
    <case>.input        # raw tool output (required)
    <case>.expected     # desired filtered output (required)
    <case>.args         # command args that produced the input (optional)
    <case>.exit         # expected exit code (optional)
```

Example:

```
tests/
  pytest/
    all-passing.input
    all-passing.expected
    some-failures.input
    some-failures.expected
    some-failures.args       # "test_foo.py -v"
  python/
    pytest-run.input
    pytest-run.expected
    pytest-run.args          # "-m pytest test_foo.py"
    pytest-run.exit          # "1"
    mypy-run.input
    mypy-run.expected
    mypy-run.args            # "-m mypy src/"
    mypy-run.exit            # "nonzero"
```

### Discovery

The test runner scans `tests/<filter>/` for `*.input` files. Each `.input` must have a matching `.expected` file to constitute a valid test case. A `.input` without a matching `.expected` is skipped with a warning.

### Exit Code File

The `.exit` file serves two purposes: it tells the mock binary what exit code to use when emitting the test input, and it tells the test runner what exit code to expect from the filter. These are the same value because filter scripts propagate the real tool's exit code via `smsm_wrap`.

- Absent: mock exits 0, expect exit code 0
- Contains a number (e.g., `1`): mock exits with that code, expect that exact exit code
- Contains `nonzero`: mock exits 1, expect any non-zero exit code

## Subcommand Structure

```
shimsumm test                              # shorthand for "run"
shimsumm test run [<filter>]               # run tests
shimsumm test add <filter> <case> [flags]  # create a test case
shimsumm test list [<filter>] [flags]      # list test cases
shimsumm test prompt <filter>              # emit LLM prompt
```

Bare `shimsumm test` (no subcommand) and `shimsumm test <filter>` (no recognized subcommand, but matches an existing filter) both behave as `shimsumm test run`. Subcommand names (`run`, `add`, `list`, `prompt`) take precedence over filter names — a filter named `list` must be tested via `shimsumm test run list`.

## `test run`

```
shimsumm test run [<filter>]
```

Runs all test cases for all filters, or all cases for a specific filter. For each test case:

1. Read `<case>.input` for the raw output content
2. Read `<case>.exit` for the expected exit code (default: 0)
3. Read `<case>.args` for command arguments (default: none)
4. Create a temporary mock binary named `<filter>` that emits the input content and exits with the code from `.exit` (or 0 if absent)
5. Place the mock binary on PATH so the filter script finds it as the "real" binary
6. Invoke the filter script with the args from `.args` (or no args)
7. Strip the `[full output: ...]` annotation line from the output
8. Diff the filtered output against `<case>.expected`
9. Compare the exit code against the expected exit code

A test case passes when both the output matches and the exit code matches. On failure, print a unified diff (for output mismatch) and/or an exit code mismatch message. Exit 0 if all cases pass, 1 if any fail.

## `test add`

```
shimsumm test add <filter> <case> [--args "..."]
shimsumm test add <filter> <case> --from-file <path> [--args "..."]
shimsumm test add <filter> <case> --run <command...>
```

Creates a new test case. Three input sources:

### Stdin (default)

Reads stdin until EOF. Saves as `<case>.input`. If `--args` is provided, saves as `<case>.args`.

### `--from-file <path>`

Copies file contents. Saves as `<case>.input`. If `--args` is provided, saves as `<case>.args`.

### `--run <command...>`

`--run` consumes all remaining arguments as the command to execute. It is mutually exclusive with `--from-file` and `--args`.

Executes the command, captures merged stdout/stderr. Saves output as `<case>.input`. Saves argv[1:] (the command arguments after stripping argv[0]) as `<case>.args`. Saves the exit code as `<case>.exit`.

### Editor Step

After capturing input, the `add` command opens `$EDITOR` (falling back to `vi`) with a temporary file pre-populated with the raw input content. The user edits this down to the desired filtered output. On save and exit, the editor contents are saved as `<case>.expected`.

### Confirmation

Prints: `Created test: <filter>/<case> (input: N lines, expected: M lines)`

### Error Handling

- If `<case>.input` already exists: error with "test case already exists" (no silent overwrite)
- If the editor exits with a non-zero code, or if the file is empty after editing: abort and clean up any files created during this invocation (`.input`, `.args`, `.exit`)
- If `--run` command fails (non-zero exit): still capture the output (filters need to handle failure output), save the exit code to `.exit`, print a note about the non-zero exit code

## `test list`

```
shimsumm test list [<filter>] [--all] [--json]
```

### Default (human-readable)

Lists filters that have test cases, with their cases and annotations:

```
pytest
  all-passing
  some-failures (args)

python
  pytest-run (args, exit: 1)
  mypy-run (args, exit: nonzero)
```

Parenthetical annotations indicate which optional fixture files are present.

### `--all`

Also includes filters (from `~/.config/shimsumm/filters/`) that have no test cases:

```
pytest
  all-passing
  some-failures (args)

python
  pytest-run (args, exit: 1)
  mypy-run (args, exit: nonzero)

git (no tests)
```

### `--json`

Emits structured JSON to stdout:

```json
[
  {
    "filter": "pytest",
    "cases": [
      {"name": "all-passing"},
      {"name": "some-failures", "args": true}
    ]
  },
  {
    "filter": "python",
    "cases": [
      {"name": "pytest-run", "args": true, "exit": "1"},
      {"name": "mypy-run", "args": true, "exit": "nonzero"}
    ]
  }
]
```

The `exit` field is present only when a `.exit` file exists, and its value matches the file content.

`--json` always includes all filters (including those with no test cases, which appear with an empty `cases` array). The `--all` flag is only relevant for human-readable output.

## `test prompt`

```
shimsumm test prompt <filter>
```

Emits a self-contained prompt to stdout for use with LLM coding agents. The prompt contains:

- Brief explanation of shimsumm: what it is, how filter scripts work
- The `smsm_filter()` contract: reads stdin (raw tool output), writes stdout (filtered output)
- Path to the filter script: `~/.config/shimsumm/filters/<filter>`
- Filter script skeleton (source wrap, define `smsm_filter`, call `smsm_wrap`)
- If a filter already exists, instruction to modify it rather than start from scratch
- How to run tests: `shimsumm test run <filter>`
- How to interpret test output (diffs show expected vs actual, exit code mismatches are reported)
- The iteration loop: edit filter, run tests, read failures, repeat

The prompt does NOT include test fixture data. The agent reads failures by running the tests.

## Help Text

`shimsumm test --help` documents both the subcommands and the intended workflow:

```
Usage: shimsumm test [run|add|list|prompt] ...

Develop and test filter scripts.

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
     until the tests pass.
```

## Implementation Notes

- The test runner continues to use end-to-end execution through `smsm_wrap` with a mock binary, not isolated `smsm_filter` invocation. This catches arg-dependent branching and integration issues.
- The mock binary exits with the code from `.exit` (or 0 if absent), regardless of what args it receives.
- `$EDITOR` falls back to `vi` when unset.
- All paths respect `$XDG_CONFIG_HOME` as the existing codebase does.
- The old flat test format (`tests/<filter>.input`, `tests/<filter>.expected`) is superseded by the new directory-based format. Existing fixtures must be migrated manually.
