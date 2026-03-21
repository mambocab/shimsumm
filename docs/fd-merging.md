# Stdout/Stderr Merging Trade-off

## What happens

`smsm_wrap` runs the wrapped command with stdout and stderr merged into a
single temp file:

```sh
"$_smsm_real" "$@" > "$_smsm_outfile" 2>&1
```

The user-defined `smsm_filter` function then receives this merged stream as
its stdin. Both file descriptors are combined before the filter ever sees the
output.

## Why

Merging the streams is a deliberate choice:

- **Preserves ordering.** The filter sees all output in the order it was
  actually produced. Keeping streams separate would lose the true interleaving
  between stdout and stderr lines.
- **Avoids doubling complexity.** Separate streams would require either two
  filter invocations (one per fd) or two temp files with no reliable way to
  reconstruct the original ordering.
- **Keeps the POSIX sh implementation simple.** `smsm_wrap` is sourced into
  plain `/bin/sh` shim scripts. Managing two streams, two temp files, and
  merging them back together would add substantial complexity for a portable
  shell function.

## Consequences

- **Structured stdout is mixed with diagnostics.** Tools that write
  machine-readable output (JSON, CSV, etc.) to stdout and human-readable
  messages to stderr will have both combined after shimming.
- **Caller redirects on stderr don't work as expected.**
  `shimmed-tool 2>/dev/null` has no effect because stderr was already merged
  into the temp file inside the shim, before the caller's redirect applies.
- **Filters can't target a single stream.** A filter has no way to know which
  lines came from stdout vs stderr.
- **Pipeline breakage.** Piping a shimmed tool's stdout into another program
  may include unexpected stderr content.

## Mitigations

- **Exclude tools from shimming.** Set `SHIMSUMM_DONT_SHIM=tool1:tool2` to
  bypass the shim for tools where fd separation matters, or set
  `SHIMSUMM_ONLY_SHIM` to an allowlist.
- **Avoid shimming pipeline participants.** If a tool's stdout feeds into
  another program, exclude it from shimming.
- **Recover full output.** The unfiltered merged output is preserved in the
  temp file. After every invocation, shimsumm prints the path to stderr:
  `[full output: /path/to/file]`.

## Future considerations

Separate-stream support (e.g., giving the filter access to stdout and stderr
independently) is theoretically possible but would significantly complicate the
filter API and the POSIX sh plumbing. It is not currently planned.
