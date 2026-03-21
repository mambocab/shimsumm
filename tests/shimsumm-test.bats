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

@test "test list: shows filters with cases" {
  run shimsumm test list
  assert_success
  assert_output --partial "mytool"
  assert_output --partial "basic"
}

@test "test list: shows annotations for args and exit" {
  printf -- '-v' > "$TESTS_DIR/mytool/basic.args"
  printf '1' > "$TESTS_DIR/mytool/basic.exit"
  run shimsumm test list
  assert_success
  assert_output --partial "(args, exit: 1)"
}

@test "test list: filter argument limits output" {
  mkdir -p "$TESTS_DIR/othertool"
  printf 'x\n' > "$TESTS_DIR/othertool/case1.input"
  printf 'x\n' > "$TESTS_DIR/othertool/case1.expected"
  run shimsumm test list mytool
  assert_success
  assert_output --partial "mytool"
  refute_output --partial "othertool"
}

@test "test list --all: includes filters with no tests" {
  cat > "$FILTERS_DIR/notested" <<'FILTEREOF'
#!/bin/sh
eval "$(shimsumm emit-wrap)"
smsm_wrap "$@"
FILTEREOF
  chmod +x "$FILTERS_DIR/notested"
  run shimsumm test list --all
  assert_success
  assert_output --partial "notested (no tests)"
}

@test "test list --json: emits JSON" {
  run shimsumm test list --json
  assert_success
  # Should be valid JSON with filter and cases
  echo "$output" | python3 -c "import sys,json; d=json.load(sys.stdin); assert any(f['filter']=='mytool' for f in d)"
}

@test "test list --json: includes exit field when .exit exists" {
  printf '1' > "$TESTS_DIR/mytool/basic.exit"
  run shimsumm test list --json
  assert_success
  echo "$output" | python3 -c "
import sys,json
d=json.load(sys.stdin)
mytool = [f for f in d if f['filter']=='mytool'][0]
case = [c for c in mytool['cases'] if c['name']=='basic'][0]
assert case['exit'] == '1', f'got {case}'
"
}

@test "test list --json: includes untested filters" {
  cat > "$FILTERS_DIR/notested" <<'FILTEREOF'
#!/bin/sh
eval "$(shimsumm emit-wrap)"
smsm_wrap "$@"
FILTEREOF
  chmod +x "$FILTERS_DIR/notested"
  run shimsumm test list --json
  assert_success
  echo "$output" | python3 -c "
import sys,json
d=json.load(sys.stdin)
notested = [f for f in d if f['filter']=='notested'][0]
assert notested['cases'] == [], f'got {notested}'
"
}

@test "test add: creates test case from --from-file" {
  printf 'raw input data\n' > "$TEST_TMP/source.txt"
  export EDITOR="cp $TEST_TMP/expected_content"
  printf 'expected output\n' > "$TEST_TMP/expected_content"
  # "y" confirms editor prompt
  run bash -c 'echo y | shimsumm test add mytool newcase --from-file "$1"' _ "$TEST_TMP/source.txt"
  assert_success
  assert_output --partial "Created test: mytool/newcase"
  [ -f "$TESTS_DIR/mytool/newcase.input" ]
  [ -f "$TESTS_DIR/mytool/newcase.expected" ]
  [ "$(cat "$TESTS_DIR/mytool/newcase.input")" = "raw input data" ]
}

@test "test add: saves .args when --args provided" {
  printf 'input data\n' > "$TEST_TMP/source.txt"
  export EDITOR="cp $TEST_TMP/expected_content"
  printf 'expected\n' > "$TEST_TMP/expected_content"
  run bash -c 'echo y | shimsumm test add mytool argscase --from-file "$1" --args "-v --flag"' _ "$TEST_TMP/source.txt"
  assert_success
  [ -f "$TESTS_DIR/mytool/argscase.args" ]
  [ "$(cat "$TESTS_DIR/mytool/argscase.args")" = "-v --flag" ]
}

@test "test add: errors if case already exists" {
  run shimsumm test add mytool basic
  assert_failure
  assert_output --partial "already exists"
}

@test "test add: creates filter directory if needed" {
  printf 'input\n' > "$TEST_TMP/source.txt"
  export EDITOR="cp $TEST_TMP/expected_content"
  printf 'expected\n' > "$TEST_TMP/expected_content"
  # Two "y" answers: create filter + open editor
  run bash -c 'printf "y\ny\n" | shimsumm test add brandnew case1 --from-file "$1"' _ "$TEST_TMP/source.txt"
  assert_success
  [ -d "$TESTS_DIR/brandnew" ]
}

@test "test add --run: captures command output and exit code" {
  export EDITOR="cp $TEST_TMP/expected_content"
  printf 'expected\n' > "$TEST_TMP/expected_content"
  # Name the script "mytool" so it matches the filter name (no mismatch prompt)
  cat > "$TEST_TMP/mytool" <<'EOF'
#!/bin/sh
echo "command output"
exit 1
EOF
  chmod +x "$TEST_TMP/mytool"
  # "y" confirms editor prompt
  run bash -c 'echo y | shimsumm test add mytool runcmd --run "$1"' _ "$TEST_TMP/mytool"
  assert_success
  [ "$(cat "$TESTS_DIR/mytool/runcmd.input")" = "command output" ]
  [ "$(cat "$TESTS_DIR/mytool/runcmd.exit")" = "1" ]
}

@test "test add: aborts on empty editor output" {
  printf 'input\n' > "$TEST_TMP/source.txt"
  export EDITOR="cp /dev/null"
  # "y" confirms editor prompt, but editor produces empty output
  run bash -c 'echo y | shimsumm test add mytool emptycase --from-file "$1"' _ "$TEST_TMP/source.txt"
  assert_failure
  [ ! -f "$TESTS_DIR/mytool/emptycase.input" ]
}

@test "test add: prompts to create missing filter" {
  printf 'input\n' > "$TEST_TMP/source.txt"
  # Answer "n" to create-filter prompt — should abort
  run bash -c 'echo n | shimsumm test add noshim case1 --from-file "$1"' _ "$TEST_TMP/source.txt"
  assert_failure
  assert_output --partial "aborted"
}

@test "test add: prompts before opening editor" {
  printf 'input\n' > "$TEST_TMP/source.txt"
  export EDITOR="cp $TEST_TMP/expected_content"
  printf 'expected\n' > "$TEST_TMP/expected_content"
  # Answer "n" to editor prompt — should abort
  run bash -c 'echo n | shimsumm test add mytool editortest --from-file "$1"' _ "$TEST_TMP/source.txt"
  assert_failure
  assert_output --partial "aborted"
  [ ! -f "$TESTS_DIR/mytool/editortest.expected" ]
}

@test "test add --run: warns on command/filter name mismatch" {
  export EDITOR="cp $TEST_TMP/expected_content"
  printf 'expected\n' > "$TEST_TMP/expected_content"
  cat > "$TEST_TMP/othercmd" <<'EOF'
#!/bin/sh
echo "output"
EOF
  chmod +x "$TEST_TMP/othercmd"
  # "y" to mismatch prompt + "y" to editor prompt
  run bash -c 'printf "y\ny\n" | shimsumm test add mytool mismatch --run "$1"' _ "$TEST_TMP/othercmd"
  assert_success
  assert_output --partial "doesn't match"
}

@test "test prompt: emits prompt with filter name" {
  run shimsumm test prompt mytool
  assert_success
  assert_output --partial "mytool"
  assert_output --partial "smsm_filter"
  assert_output --partial "smsm_wrap"
  assert_output --partial "shimsumm test run mytool"
}

@test "test prompt: includes filter path" {
  run shimsumm test prompt mytool
  assert_success
  assert_output --partial "filters/mytool"
}

@test "test prompt: mentions existing filter when present" {
  run shimsumm test prompt mytool
  assert_success
  assert_output --partial "Modify"
}

@test "test prompt: includes skeleton when no filter exists" {
  run shimsumm test prompt newfilter
  assert_success
  assert_output --partial "smsm_filter"
}
