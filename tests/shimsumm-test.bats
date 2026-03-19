#!/usr/bin/env bats
load 'test_helper'

setup() {
  TEST_TMP=$(mktemp -d)
  export XDG_CONFIG_HOME="$TEST_TMP/config"
  FILTERS_DIR="$XDG_CONFIG_HOME/shimsumm/filters"
  TESTS_DIR="$XDG_CONFIG_HOME/shimsumm/tests"
  mkdir -p "$FILTERS_DIR" "$TESTS_DIR"

  # Filter script: keeps only lines matching KEEP
  cat > "$FILTERS_DIR/mytool" <<'EOF'
#!/bin/sh
eval "$(shimsumm emit-wrap)"
smsm_filter() { grep KEEP || true; }
smsm_wrap "$@"
EOF
  chmod +x "$FILTERS_DIR/mytool"

  # Fixtures
  printf 'KEEP this line\nDROP this line\n' > "$TESTS_DIR/mytool.input"
  printf 'KEEP this line\n' > "$TESTS_DIR/mytool.expected"

  export PATH="$FILTERS_DIR:$PROJECT_ROOT/bin:$PATH"
}

teardown() {
  rm -rf "$TEST_TMP"
}

@test "shimsumm-test exits 0 when output matches expected" {
  run shimsumm test mytool
  assert_success
}

@test "shimsumm-test exits 1 when output does not match expected" {
  printf 'WRONG expected output\n' > "$XDG_CONFIG_HOME/shimsumm/tests/mytool.expected"
  run shimsumm test mytool
  assert_failure
}

@test "shimsumm-test shows diff on mismatch" {
  printf 'WRONG\n' > "$XDG_CONFIG_HOME/shimsumm/tests/mytool.expected"
  run shimsumm test mytool
  assert_output --partial "KEEP"
  assert_output --partial "WRONG"
}

@test "shimsumm-test exits 127 when no filter exists for tool" {
  run -127 shimsumm test notarealtool
}

@test "shimsumm-test exits 1 when no fixtures found for tool" {
  # Create filter with no fixtures
  printf '#!/bin/sh\neval "$(shimsumm emit-wrap)"\nsmsm_wrap "$@"\n' \
    > "$XDG_CONFIG_HOME/shimsumm/filters/mytool_nofixtures"
  chmod +x "$XDG_CONFIG_HOME/shimsumm/filters/mytool_nofixtures"
  run shimsumm test mytool_nofixtures
  assert_failure
  assert_output --partial "no fixtures"
}

@test "shimsumm-test with no args runs all filters that have fixtures" {
  # Second filter with matching fixtures
  cat > "$XDG_CONFIG_HOME/shimsumm/filters/othertool" <<'EOF'
#!/bin/sh
eval "$(shimsumm emit-wrap)"
smsm_wrap "$@"
EOF
  chmod +x "$XDG_CONFIG_HOME/shimsumm/filters/othertool"
  printf 'some input\n' > "$XDG_CONFIG_HOME/shimsumm/tests/othertool.input"
  printf 'some input\n' > "$XDG_CONFIG_HOME/shimsumm/tests/othertool.expected"
  run shimsumm test
  assert_success
  assert_output --partial "PASS: mytool"
  assert_output --partial "PASS: othertool"
}
