#!/usr/bin/env bats
load 'test_helper'

setup() {
  TEST_TMP=$(mktemp -d)
  export XDG_CONFIG_HOME="$TEST_TMP/config"
  FILTERS_DIR="$XDG_CONFIG_HOME/shimsumm/filters"
  TESTS_DIR="$XDG_CONFIG_HOME/shimsumm/tests"
  mkdir -p "$FILTERS_DIR" "$TESTS_DIR/mytool"

  # Filter script: keeps only lines matching KEEP
  cat > "$FILTERS_DIR/mytool" <<'FILTEREOF'
#!/bin/sh
eval "$(shimsumm emit-wrap)"
smsm_filter() { grep KEEP || true; }
smsm_wrap "$@"
FILTEREOF
  chmod +x "$FILTERS_DIR/mytool"

  # Directory-based fixtures
  printf 'KEEP this line\nDROP this line\n' > "$TESTS_DIR/mytool/basic.input"
  printf 'KEEP this line\n' > "$TESTS_DIR/mytool/basic.expected"

  export PATH="$FILTERS_DIR:$PROJECT_ROOT/bin:$PATH"
}

teardown() {
  rm -rf "$TEST_TMP"
}

@test "test run: exits 0 when output matches expected" {
  run shimsumm test run mytool
  assert_success
  assert_output --partial "PASS: mytool/basic"
}

@test "test run: exits 1 when output does not match expected" {
  printf 'WRONG expected output\n' > "$TESTS_DIR/mytool/basic.expected"
  run shimsumm test run mytool
  assert_failure
  assert_output --partial "FAIL: mytool/basic"
}

@test "test run: shows diff on mismatch" {
  printf 'WRONG\n' > "$TESTS_DIR/mytool/basic.expected"
  run shimsumm test run mytool
  assert_output --partial "KEEP"
  assert_output --partial "WRONG"
}

@test "test run: multiple cases in one filter" {
  printf 'KEEP second\nDROP second\n' > "$TESTS_DIR/mytool/second.input"
  printf 'KEEP second\n' > "$TESTS_DIR/mytool/second.expected"
  run shimsumm test run mytool
  assert_success
  assert_output --partial "PASS: mytool/basic"
  assert_output --partial "PASS: mytool/second"
}

@test "test run: warns on .input without .expected" {
  printf 'orphan input\n' > "$TESTS_DIR/mytool/orphan.input"
  run shimsumm test run mytool
  assert_success
  assert_output --partial "PASS: mytool/basic"
  # Warning goes to stderr — check combined
  assert_output --partial "warning"
}

@test "test run: exit code from .exit file (numeric)" {
  mkdir -p "$TESTS_DIR/mytool2"
  cat > "$FILTERS_DIR/mytool2" <<'FILTEREOF'
#!/bin/sh
eval "$(shimsumm emit-wrap)"
smsm_wrap "$@"
FILTEREOF
  chmod +x "$FILTERS_DIR/mytool2"
  printf 'some output\n' > "$TESTS_DIR/mytool2/failing.input"
  printf 'some output\n' > "$TESTS_DIR/mytool2/failing.expected"
  printf '1' > "$TESTS_DIR/mytool2/failing.exit"
  run shimsumm test run mytool2
  assert_success
}

@test "test run: exit code mismatch fails" {
  mkdir -p "$TESTS_DIR/exitmismatch"
  # Filter that always exits 0 regardless of wrapped tool's exit code
  cat > "$FILTERS_DIR/exitmismatch" <<'FILTEREOF'
#!/bin/sh
eval "$(shimsumm emit-wrap)"
smsm_wrap "$@"
exit 0
FILTEREOF
  chmod +x "$FILTERS_DIR/exitmismatch"
  printf 'some output\n' > "$TESTS_DIR/exitmismatch/failing.input"
  printf 'some output\n' > "$TESTS_DIR/exitmismatch/failing.expected"
  printf '2' > "$TESTS_DIR/exitmismatch/failing.exit"
  run shimsumm test run exitmismatch
  assert_failure
  assert_output --partial "exit code"
}

@test "test run: nonzero exit code" {
  mkdir -p "$TESTS_DIR/mytool2"
  cat > "$FILTERS_DIR/mytool2" <<'FILTEREOF'
#!/bin/sh
eval "$(shimsumm emit-wrap)"
smsm_wrap "$@"
FILTEREOF
  chmod +x "$FILTERS_DIR/mytool2"
  printf 'some output\n' > "$TESTS_DIR/mytool2/case1.input"
  printf 'some output\n' > "$TESTS_DIR/mytool2/case1.expected"
  printf 'nonzero' > "$TESTS_DIR/mytool2/case1.exit"
  run shimsumm test run mytool2
  assert_success
}

@test "test run: args from .args file" {
  printf 'KEEP this line\nDROP this line\n' > "$TESTS_DIR/mytool/withargs.input"
  printf 'KEEP this line\n' > "$TESTS_DIR/mytool/withargs.expected"
  printf -- '-v --flag' > "$TESTS_DIR/mytool/withargs.args"
  run shimsumm test run mytool
  assert_success
  assert_output --partial "PASS: mytool/withargs"
}

@test "test run: no args runs all filters" {
  mkdir -p "$TESTS_DIR/othertool"
  cat > "$FILTERS_DIR/othertool" <<'FILTEREOF'
#!/bin/sh
eval "$(shimsumm emit-wrap)"
smsm_wrap "$@"
FILTEREOF
  chmod +x "$FILTERS_DIR/othertool"
  printf 'some input\n' > "$TESTS_DIR/othertool/case1.input"
  printf 'some input\n' > "$TESTS_DIR/othertool/case1.expected"
  run shimsumm test run
  assert_success
  assert_output --partial "PASS: mytool/basic"
  assert_output --partial "PASS: othertool/case1"
}

@test "bare 'shimsumm test' runs all tests" {
  run shimsumm test
  assert_success
  assert_output --partial "PASS: mytool/basic"
}

@test "'shimsumm test <filter>' routes to test run" {
  run shimsumm test mytool
  assert_success
  assert_output --partial "PASS: mytool/basic"
}
