#!/usr/bin/env bats
load 'test_helper'

setup() {
  TEST_TMP=$(mktemp -d)
  SAVED_PATH="$PATH"
  FILTERS_DIR="$TEST_TMP/filters"
  REAL_BIN="$TEST_TMP/real"
  mkdir -p "$FILTERS_DIR" "$REAL_BIN" /tmp/shimsumm

  # Default passthrough filter script
  cat > "$FILTERS_DIR/mytool" <<'EOF'
#!/bin/sh
eval "$(shimsumm emit-wrap)"
smsm_wrap "$@"
EOF
  chmod +x "$FILTERS_DIR/mytool"

  # Default real binary: prints "real output" and exits 0
  cat > "$REAL_BIN/mytool" <<'EOF'
#!/bin/sh
printf 'real output\n'
exit 0
EOF
  chmod +x "$REAL_BIN/mytool"

  export PATH="$FILTERS_DIR:$REAL_BIN:$PROJECT_ROOT/bin:$PATH"
}

teardown() {
  export PATH="$SAVED_PATH"
  rm -rf "$TEST_TMP"
}

@test "smsm_wrap exits 0 when real tool exits 0" {
  run "$FILTERS_DIR/mytool"
  assert_equal "$status" 0
}

@test "smsm_wrap passes through non-zero exit code" {
  printf '#!/bin/sh\nexit 42\n' > "$REAL_BIN/mytool"
  run "$FILTERS_DIR/mytool"
  assert_equal "$status" 42
}

@test "smsm_wrap pipes output through smsm_filter when defined" {
  cat > "$FILTERS_DIR/mytool" <<'EOF'
#!/bin/sh
eval "$(shimsumm emit-wrap)"
smsm_filter() { grep KEEP; }
smsm_wrap "$@"
EOF
  printf '#!/bin/sh\nprintf "KEEP this\nDROP this\n"\n' > "$REAL_BIN/mytool"
  run "$FILTERS_DIR/mytool"
  assert_output --partial "KEEP this"
  refute_output --partial "DROP this"
}

@test "smsm_wrap uses cat when smsm_filter not defined" {
  printf '#!/bin/sh\nprintf "all output\n"\n' > "$REAL_BIN/mytool"
  run "$FILTERS_DIR/mytool"
  assert_output --partial "all output"
}

@test "smsm_wrap saves full output to temp file" {
  cat > "$FILTERS_DIR/mytool" <<'EOF'
#!/bin/sh
eval "$(shimsumm emit-wrap)"
smsm_filter() { printf 'filtered\n'; }
smsm_wrap "$@"
EOF
  printf '#!/bin/sh\nprintf "original content\n"\n' > "$REAL_BIN/mytool"
  run "$FILTERS_DIR/mytool"
  # Extract temp file path from [full output: ...] annotation
  local tmpfile
  tmpfile=$(printf '%s\n' "$output" | grep '^\[full output:' | sed 's/\[full output: //;s/\]//')
  assert [ -f "$tmpfile" ]
  run cat "$tmpfile"
  assert_output --partial "original content"
}

@test "smsm_wrap appends annotation with temp file path" {
  run "$FILTERS_DIR/mytool"
  assert_output --partial "[full output: /tmp/shimsumm/mytool-"
  # annotation must include a timestamp segment (YYYYmmddHHMMSS = 14 digits)
  run sh -c 'printf "%s\n" "$1" | grep -qE "\\[full output: /tmp/shimsumm/mytool-[0-9]{14}-[0-9]"' \
    _ "$output"
  assert_success
}

@test "smsm_wrap merges stderr into output" {
  printf '#!/bin/sh\nprintf "stdout\n"; printf "stderr\n" >&2\n' > "$REAL_BIN/mytool"
  run "$FILTERS_DIR/mytool"
  assert_output --partial "stdout"
  assert_output --partial "stderr"
}

@test "smsm_wrap passes stdin to real tool" {
  printf '#!/bin/sh\ncat\n' > "$REAL_BIN/mytool"
  run bash -c 'printf "hello from stdin\n" | "$1"' _ "$FILTERS_DIR/mytool"
  assert_output --partial "hello from stdin"
}

@test "smsm_wrap exits 127 when real binary not found" {
  # Only filters dir and wrap lib on PATH — no real binary
  export PATH="$FILTERS_DIR:$PROJECT_ROOT/bin"
  run -127 "$FILTERS_DIR/mytool"
}

@test "smsm_wrap finds real binary when filters dir not in PATH" {
  # Simulate dispatcher call: filters dir not on PATH
  export PATH="$REAL_BIN:$PROJECT_ROOT/bin:/bin:/usr/bin"
  run "$FILTERS_DIR/mytool"
  assert_equal "$status" 0
  assert_output --partial "real output"
}
