#!/usr/bin/env bats
load 'test_helper'

setup() {
  TEST_TMP=$(mktemp -d)
  export XDG_CONFIG_HOME="$TEST_TMP/config"
  FILTERS_DIR="$XDG_CONFIG_HOME/shimsumm/filters"
  mkdir -p "$FILTERS_DIR"

  # Put shimsumm on PATH
  export PATH="$FILTERS_DIR:$PROJECT_ROOT/bin:$PATH"
}

teardown() {
  rm -rf "$TEST_TMP"
}

# Helper to create a valid filter
create_valid_filter() {
  local name="${1:-mytool}"
  cat > "$FILTERS_DIR/$name" <<'EOF'
#!/bin/sh
eval "$(shimsumm emit-wrap)"
smsm_wrap "$@"
EOF
  chmod +x "$FILTERS_DIR/$name"
}

# ---- ENV checks ----

@test "doctor: all ENV checks pass in valid setup (verbose)" {
  create_valid_filter
  run shimsumm doctor -v
  assert_success
  assert_line "ENV: OK: filters directory exists"
  assert_line "ENV: OK: filters directory on PATH"
  assert_line "ENV: OK: shimsumm on PATH"
}

@test "doctor: ENV FAIL when filters dir missing" {
  rm -rf "$FILTERS_DIR"
  run shimsumm doctor
  assert_failure
  assert_output --partial "ENV: FAIL: filters directory missing"
}

@test "doctor: ENV FAIL when filters dir not on PATH" {
  # Remove filters dir from PATH
  export PATH="$PROJECT_ROOT/bin:/usr/bin:/bin"
  create_valid_filter
  run shimsumm doctor
  assert_failure
  assert_output --partial "ENV: FAIL: filters directory not on PATH"
}

@test "doctor: ENV FAIL when shimsumm not on PATH" {
  # Remove shimsumm from PATH, keep filters dir; invoke via absolute path
  export PATH="$FILTERS_DIR:/usr/bin:/bin"
  create_valid_filter
  run "$PROJECT_ROOT/bin/shimsumm" doctor
  assert_failure
  assert_output --partial "ENV: FAIL: shimsumm not found on PATH"
}

# ---- Per-filter checks ----

@test "doctor: valid filter passes all checks" {
  create_valid_filter
  run shimsumm doctor
  assert_success
  assert_line "mytool: OK"
}

@test "doctor: valid filter verbose shows all checks" {
  create_valid_filter
  run shimsumm doctor -v
  assert_success
  assert_line "mytool: OK: executable"
  assert_line "mytool: OK: shebang present"
  assert_line "mytool: OK: sources shimsumm emit-wrap"
  assert_line "mytool: OK: calls smsm_wrap"
  assert_line "mytool: OK: syntax ok"
  assert_line "mytool: OK: sources cleanly"
}

@test "doctor: FAIL not executable" {
  create_valid_filter
  chmod -x "$FILTERS_DIR/mytool"
  run shimsumm doctor
  assert_failure
  assert_line "mytool: FAIL: not executable"
}

@test "doctor: FAIL no shebang" {
  cat > "$FILTERS_DIR/mytool" <<'EOF'
eval "$(shimsumm emit-wrap)"
smsm_wrap "$@"
EOF
  chmod +x "$FILTERS_DIR/mytool"
  run shimsumm doctor
  assert_failure
  assert_line "mytool: FAIL: no shebang"
}

@test "doctor: FAIL missing sources-wrap" {
  cat > "$FILTERS_DIR/mytool" <<'EOF'
#!/bin/sh
smsm_wrap "$@"
EOF
  chmod +x "$FILTERS_DIR/mytool"
  run shimsumm doctor
  assert_failure
  assert_line "mytool: FAIL: does not source shimsumm emit-wrap"
}

@test "doctor: FAIL missing calls-wrap" {
  cat > "$FILTERS_DIR/mytool" <<'EOF'
#!/bin/sh
eval "$(shimsumm emit-wrap)"
EOF
  chmod +x "$FILTERS_DIR/mytool"
  run shimsumm doctor
  assert_failure
  assert_output --partial 'mytool: FAIL: does not call smsm_wrap "$@"'
}

@test "doctor: FAIL syntax error" {
  cat > "$FILTERS_DIR/mytool" <<'EOF'
#!/bin/sh
eval "$(shimsumm emit-wrap)"
if then fi
smsm_wrap "$@"
EOF
  chmod +x "$FILTERS_DIR/mytool"
  run shimsumm doctor
  assert_failure
  assert_line "mytool: FAIL: syntax error"
}

@test "doctor: FAIL source error" {
  cat > "$FILTERS_DIR/mytool" <<'EOF'
#!/bin/sh
eval "$(shimsumm emit-wrap)"
exit 1
smsm_wrap "$@"
EOF
  chmod +x "$FILTERS_DIR/mytool"
  run shimsumm doctor
  assert_failure
  assert_line "mytool: FAIL: source error"
}

# ---- Exception comments ----

@test "doctor: skip comment suppresses check" {
  cat > "$FILTERS_DIR/mytool" <<'EOF'
#!/bin/sh
# shimsumm-doctor: skip shebang
eval "$(shimsumm emit-wrap)"
smsm_wrap "$@"
EOF
  chmod +x "$FILTERS_DIR/mytool"
  run shimsumm doctor -v
  assert_success
  assert_line "mytool: SKIP: shebang"
}

@test "doctor: skip comment with multiple check names" {
  cat > "$FILTERS_DIR/mytool" <<'EOF'
#!/bin/sh
# shimsumm-doctor: skip shebang,syntax
eval "$(shimsumm emit-wrap)"
smsm_wrap "$@"
EOF
  chmod +x "$FILTERS_DIR/mytool"
  run shimsumm doctor -v
  assert_success
  assert_line "mytool: SKIP: shebang"
  assert_line "mytool: SKIP: syntax"
}

@test "doctor: unknown check name in skip comment produces warning" {
  cat > "$FILTERS_DIR/mytool" <<'EOF'
#!/bin/sh
# shimsumm-doctor: skip boguscheck
eval "$(shimsumm emit-wrap)"
smsm_wrap "$@"
EOF
  chmod +x "$FILTERS_DIR/mytool"
  run shimsumm doctor
  assert_output --partial "WARN: unknown check name in skip comment: boguscheck"
}

@test "doctor: all checks skipped shows SKIP" {
  cat > "$FILTERS_DIR/mytool" <<'EOF'
#!/bin/sh
# shimsumm-doctor: skip executable,shebang,sources-wrap,calls-wrap,syntax,sources-cleanly
eval "$(shimsumm emit-wrap)"
smsm_wrap "$@"
EOF
  chmod +x "$FILTERS_DIR/mytool"
  run shimsumm doctor
  assert_success
  assert_line "mytool: SKIP"
}

# ---- Summary and exit ----

@test "doctor: summary line counts" {
  create_valid_filter mytool
  cat > "$FILTERS_DIR/badtool" <<'EOF'
badcontent
EOF
  chmod +x "$FILTERS_DIR/badtool"
  run shimsumm doctor
  assert_failure
  assert_line "2 filters checked, 1 passed, 1 failed"
}

@test "doctor: exit 0 when all pass" {
  create_valid_filter
  run shimsumm doctor
  assert_success
}

@test "doctor: exit 1 when any fail" {
  create_valid_filter mytool
  cat > "$FILTERS_DIR/badtool" <<'EOF'
badcontent
EOF
  run shimsumm doctor
  assert_failure
}

# ---- Default vs verbose ----

@test "doctor: default mode suppresses ENV OK lines" {
  create_valid_filter
  run shimsumm doctor
  assert_success
  refute_output --partial "ENV: OK"
}

@test "doctor: default mode shows only OK per passing filter (not individual checks)" {
  create_valid_filter
  run shimsumm doctor
  assert_success
  assert_line "mytool: OK"
  refute_output --partial "mytool: OK:"
}

@test "doctor: verbose mode shows individual checks" {
  create_valid_filter
  run shimsumm doctor --verbose
  assert_success
  assert_line "mytool: OK: executable"
}
