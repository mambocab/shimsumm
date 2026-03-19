#!/usr/bin/env bats
load 'test_helper'

setup() {
  TEST_TMP=$(mktemp -d)
  export XDG_CONFIG_HOME="$TEST_TMP/config"
  FILTERS_DIR="$XDG_CONFIG_HOME/shimsumm/filters"
  REAL_BIN="$TEST_TMP/real"
  mkdir -p "$FILTERS_DIR" "$REAL_BIN"

  # Filter script
  cat > "$FILTERS_DIR/mytool" <<'EOF'
#!/bin/sh
eval "$(shimsumm emit-wrap)"
smsm_wrap "$@"
EOF
  chmod +x "$FILTERS_DIR/mytool"

  # Real binary
  printf '#!/bin/sh\nprintf "real output\n"\nexit 0\n' > "$REAL_BIN/mytool"
  chmod +x "$REAL_BIN/mytool"

  export PATH="$FILTERS_DIR:$REAL_BIN:$PROJECT_ROOT/bin:$PATH"
}

teardown() {
  rm -rf "$TEST_TMP"
}

@test "dispatch routes to filter script" {
  run shimsumm dispatch mytool
  assert_output --partial "real output"
}

@test "dispatch passes arguments to filter script" {
  cat > "$REAL_BIN/mytool" <<'EOF'
#!/bin/sh
printf "args: %s\n" "$*"
exit 0
EOF
  chmod +x "$REAL_BIN/mytool"
  run shimsumm dispatch mytool foo bar
  assert_output --partial "args: foo bar"
}

@test "dispatch passes through exit code" {
  cat > "$REAL_BIN/mytool" <<'EOF'
#!/bin/sh
exit 7
EOF
  chmod +x "$REAL_BIN/mytool"
  run shimsumm dispatch mytool
  assert_equal "$status" 7
}

@test "dispatch exits 1 with no tool argument" {
  run shimsumm dispatch
  assert_equal "$status" 1
  assert_output --partial "Usage"
}

@test "dispatch exits 127 for unknown tool" {
  run -127 shimsumm dispatch notarealtool
  assert_output --partial "notarealtool"
}

@test "shimsumm exits 1 with no arguments" {
  run shimsumm
  assert_equal "$status" 1
  assert_output --partial "Usage"
}

@test "shimsumm exits 1 for unknown subcommand" {
  run shimsumm notasubcommand
  assert_equal "$status" 1
  assert_output --partial "notasubcommand"
}
