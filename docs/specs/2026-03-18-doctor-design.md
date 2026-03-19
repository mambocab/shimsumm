# shimsumm doctor — Design Spec

Date: 2026-03-18

## Overview

`shimsumm doctor` validates a user's filter configuration. It checks the
environment and each filter script, reports findings, and exits non-zero if
anything fails.

## Invocation

```sh
shimsumm doctor [-v | --verbose]
```

No positional arguments. Checks all filters.

## Architecture

A new cobra subcommand `doctor` in `cmd/shimsumm/main.go`. Calls a
`cmdDoctor(verbose bool)` function that runs environment checks, then per-filter
checks, collects pass/fail counts, and exits 0 only if all checks pass.

## ENV Checks (run once)

1. Filters directory exists (`getFiltersDir()`)
2. Filters directory is on `$PATH`
3. `shimsumm` binary is findable on `$PATH` (filter scripts need it for
   `eval "$(shimsumm emit-wrap)"`)

ENV checks run report-and-continue: failures do not abort per-filter checks.

## Per-Filter Checks (for each file in filters directory)

1. **executable** — file has executable bit set
2. **shebang** — first line starts with `#!`
3. **sources-wrap** — file contains `shimsumm emit-wrap`
4. **calls-wrap** — file contains `smsm_wrap "$@"`
5. **syntax** — `sh -n <file>` exits 0
6. **sources-cleanly** — source the file in a subshell with a stub that defines
   `smsm_wrap` as a no-op, verify exit 0. The stub is a temp script containing
   `smsm_wrap() { return 0; }` placed first on PATH as `shimsumm` (so
   `eval "$(shimsumm emit-wrap)"` evaluates the stub instead of the real binary).

## Exception Comments

Filter scripts can suppress specific checks with comments:

```sh
# shimsumm-doctor: skip shebang,syntax
```

- Multiple skip comments per file are allowed; skip sets are unioned.
- Comma-separated check names within a single comment.
- Valid check names: `executable`, `shebang`, `sources-wrap`, `calls-wrap`,
  `syntax`, `sources-cleanly`.
- Unknown check names produce a warning line in output.
- Skipped checks do not count as failures.

## Output

### Default (quiet)

- ENV: only FAIL lines shown, successes suppressed
- Per filter with any failure: one FAIL line per failing check
- Per filter with no failures and at least one OK: one `OK` line
- Per filter with all checks skipped: one `SKIP` line
- Summary line always shown

```
ENV: FAIL: shimsumm not found on PATH
mytool: OK
badtool: FAIL: not executable
badtool: FAIL: no shebang
skippedtool: SKIP
2 filters checked, 1 passed, 1 failed
```

### Verbose (`-v` / `--verbose`)

Every individual check gets its own OK, FAIL, or SKIP line:

```
ENV: OK: filters directory exists
ENV: OK: filters directory on PATH
ENV: FAIL: shimsumm not found on PATH
mytool: OK: executable
mytool: OK: shebang present
mytool: SKIP: sources shimsumm emit-wrap
mytool: OK: calls smsm_wrap
mytool: OK: syntax ok
mytool: OK: sources cleanly
2 filters checked, 1 passed, 1 failed
```

## Exit Code

- 0 if all ENV checks pass and all filter checks pass (or are skipped)
- 1 if any ENV check or filter check fails

## Testing

New bats test file `tests/shimsumm-doctor.bats` covering:

- All ENV checks pass in valid setup (verbose output verified)
- ENV FAIL when filters dir missing
- ENV FAIL when filters dir not on PATH
- ENV FAIL when shimsumm not on PATH
- Per-filter OK for valid filter
- Per-filter FAIL for each check type (not executable, no shebang, missing
  sources-wrap, missing calls-wrap, syntax error, source error)
- Exception comment skips the check
- Exception comment with multiple check names
- Unknown check name in exception comment produces warning
- Summary line counts
- Exit 0 when all pass, exit 1 when any fail
- Default vs verbose output differences
