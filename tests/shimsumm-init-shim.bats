#!/usr/bin/env bats
load 'test_helper'

setup() {
  TEST_TMP=$(mktemp -d)
  export XDG_CONFIG_HOME="$TEST_TMP/config"
  FILTERS_DIR="$XDG_CONFIG_HOME/shimsumm/filters"
  REAL_BIN="$TEST_TMP/real"
  mkdir -p "$FILTERS_DIR" "$REAL_BIN" /tmp/shimsumm

  # Filter script that uppercases output
  cat > "$FILTERS_DIR/mytool" <<'EOF'
#!/bin/sh
eval "$(shimsumm emit-wrap)"
smsm_filter() { tr '[:lower:]' '[:upper:]'; }
smsm_wrap "$@"
EOF
  chmod +x "$FILTERS_DIR/mytool"

  # Second filter script
  cat > "$FILTERS_DIR/othertool" <<'EOF'
#!/bin/sh
eval "$(shimsumm emit-wrap)"
smsm_filter() { tr '[:lower:]' '[:upper:]'; }
smsm_wrap "$@"
EOF
  chmod +x "$FILTERS_DIR/othertool"

  # Real binaries
  printf '#!/bin/sh\nprintf "hello from mytool\n"\n' > "$REAL_BIN/mytool"
  chmod +x "$REAL_BIN/mytool"
  printf '#!/bin/sh\nprintf "hello from othertool\n"\n' > "$REAL_BIN/othertool"
  chmod +x "$REAL_BIN/othertool"

  export PATH="$FILTERS_DIR:$REAL_BIN:$PROJECT_ROOT/bin:$PATH"
}

teardown() {
  rm -rf "$TEST_TMP"
}

# ---- init output tests ----

@test "init --dont-shim emits SHIMSUMM_DONT_SHIM export" {
  run shimsumm init --dont-shim foo --dont-shim bar
  assert_output --partial 'SHIMSUMM_DONT_SHIM="foo:bar"'
  assert_output --partial 'export SHIMSUMM_DONT_SHIM'
}

@test "init --only-shim emits SHIMSUMM_ONLY_SHIM export" {
  run shimsumm init --only-shim gcc
  assert_output --partial 'SHIMSUMM_ONLY_SHIM="gcc"'
  assert_output --partial 'export SHIMSUMM_ONLY_SHIM'
}

@test "init --dont-shim with fish emits fish syntax" {
  run shimsumm init --dont-shim foo fish
  assert_output --partial 'set -gx SHIMSUMM_DONT_SHIM'
}

@test "init --only-shim with fish emits fish syntax" {
  run shimsumm init --only-shim foo fish
  assert_output --partial 'set -gx SHIMSUMM_ONLY_SHIM'
}

@test "init rejects --dont-shim and --only-shim together" {
  run shimsumm init --dont-shim foo --only-shim bar
  assert_failure
  assert_output --partial "mutually exclusive"
}

@test "init --dont-shim requires a tool name" {
  run shimsumm init --dont-shim
  assert_failure
  assert_output --partial "requires a tool name"
}

@test "init --only-shim requires a tool name" {
  run shimsumm init --only-shim
  assert_failure
  assert_output --partial "requires a tool name"
}

@test "init shell arg works alongside flags" {
  run shimsumm init --dont-shim foo bash
  assert_success
  assert_output --partial 'SHIMSUMM_DONT_SHIM="foo"'
  # Should still have the PATH setup
  assert_output --partial '_smsm_filters='
}

@test "init shell arg works before flags" {
  run shimsumm init bash --dont-shim foo
  assert_success
  assert_output --partial 'SHIMSUMM_DONT_SHIM="foo"'
}

# ---- smsm_wrap runtime behavior ----

@test "--dont-shim skips filtering for excluded tool" {
  export SHIMSUMM_DONT_SHIM="mytool"
  run "$FILTERS_DIR/mytool"
  # Should get original lowercase output (filter not applied)
  assert_output --partial "hello from mytool"
  # Should NOT have the [full output:] annotation (exec'd directly)
  refute_output --partial "[full output:"
}

@test "--dont-shim still filters non-excluded tools" {
  export SHIMSUMM_DONT_SHIM="mytool"
  run "$FILTERS_DIR/othertool"
  # othertool is not excluded, so filter applies (uppercased)
  assert_output --partial "HELLO FROM OTHERTOOL"
}

@test "--only-shim filters included tool" {
  export SHIMSUMM_ONLY_SHIM="mytool"
  run "$FILTERS_DIR/mytool"
  # mytool is in the list, so filter applies (uppercased)
  assert_output --partial "HELLO FROM MYTOOL"
}

@test "--only-shim skips filtering for non-included tool" {
  export SHIMSUMM_ONLY_SHIM="mytool"
  run "$FILTERS_DIR/othertool"
  # othertool is not in the only-shim list, so filter skipped
  assert_output --partial "hello from othertool"
  refute_output --partial "[full output:"
}

@test "--dont-shim with multiple tools excludes all listed" {
  export SHIMSUMM_DONT_SHIM="mytool:othertool"
  run "$FILTERS_DIR/mytool"
  assert_output --partial "hello from mytool"
  refute_output --partial "[full output:"
  run "$FILTERS_DIR/othertool"
  assert_output --partial "hello from othertool"
  refute_output --partial "[full output:"
}

@test "--dont-shim preserves exit code of skipped tool" {
  export SHIMSUMM_DONT_SHIM="mytool"
  printf '#!/bin/sh\nexit 3\n' > "$REAL_BIN/mytool"
  run "$FILTERS_DIR/mytool"
  assert_equal "$status" 3
}

@test "no shim env vars means normal filtering" {
  unset SHIMSUMM_DONT_SHIM 2>/dev/null || true
  unset SHIMSUMM_ONLY_SHIM 2>/dev/null || true
  run "$FILTERS_DIR/mytool"
  # Filter applied (uppercased)
  assert_output --partial "HELLO FROM MYTOOL"
  assert_output --partial "[full output:"
}
