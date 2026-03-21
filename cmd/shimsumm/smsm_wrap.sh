smsm_wrap() {
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

  # Set up temp directory for full unfiltered output
  _smsm_tmpdir="${XDG_STATE_HOME:-$HOME/.local/state}/shimsumm/tmp"
  mkdir -p "$_smsm_tmpdir"

  # Weekly cleanup: if last-cleanup is missing or older than 7 days
  _smsm_cleanup_file="$_smsm_tmpdir/last-cleanup"
  _smsm_do_cleanup=0
  if [ ! -f "$_smsm_cleanup_file" ]; then
    _smsm_do_cleanup=1
  elif [ -n "$(find "$_smsm_cleanup_file" -mtime +7 2>/dev/null)" ]; then
    _smsm_do_cleanup=1
  fi
  if [ "$_smsm_do_cleanup" = "1" ]; then
    find "$_smsm_tmpdir" -type f -name '*.??????' -mtime +7 -delete 2>/dev/null || true
    touch "$_smsm_cleanup_file"
  fi

  # Create temp file for full unfiltered output
  _smsm_outfile=$(mktemp "${_smsm_tmpdir}/${_smsm_tool}.XXXXXX")

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

  # Append annotation so user can access full output if needed (to stderr)
  printf '[full output: %s]\n' "$_smsm_outfile" >&2

  # Clean up locals
  unset _smsm_tool _smsm_filters_dir _smsm_found_filters_dir _smsm_real
  unset _smsm_saved_ifs _smsm_entry _smsm_outfile _smsm_line
  unset _smsm_tmpdir _smsm_cleanup_file _smsm_do_cleanup

  # Return original exit code (DO NOT unset before return!)
  return "$_smsm_exit_code"
}
