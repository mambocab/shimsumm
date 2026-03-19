#!/usr/bin/env bats
load 'test_helper'

setup() {
  TEST_TMP=$(mktemp -d)
  export XDG_CONFIG_HOME="$TEST_TMP/config"
  FILTERS_DIR="$XDG_CONFIG_HOME/shimsumm/filters"
  export PATH="$PROJECT_ROOT/bin:$PATH"
}

teardown() {
  rm -rf "$TEST_TMP"
}

@test "new-filter creates filter file" {
  run shimsumm new-filter mytool
  assert_success
  assert_output --partial "created"
  assert [ -f "$FILTERS_DIR/mytool" ]
}

@test "new-filter file is executable" {
  shimsumm new-filter mytool
  assert [ -x "$FILTERS_DIR/mytool" ]
}

@test "new-filter file has shebang" {
  shimsumm new-filter mytool
  head -1 "$FILTERS_DIR/mytool" | grep -q '^#!/bin/sh'
}

@test "new-filter file sources emit-wrap" {
  shimsumm new-filter mytool
  grep -q 'shimsumm emit-wrap' "$FILTERS_DIR/mytool"
}

@test "new-filter file defines smsm_filter" {
  shimsumm new-filter mytool
  grep -q 'smsm_filter()' "$FILTERS_DIR/mytool"
}

@test "new-filter file calls smsm_wrap" {
  shimsumm new-filter mytool
  grep -q 'smsm_wrap "$@"' "$FILTERS_DIR/mytool"
}

@test "new-filter creates filters directory if missing" {
  assert [ ! -d "$FILTERS_DIR" ]
  shimsumm new-filter mytool
  assert [ -d "$FILTERS_DIR" ]
}

@test "new-filter refuses to overwrite existing filter" {
  shimsumm new-filter mytool
  run shimsumm new-filter mytool
  assert_failure
  assert_output --partial "already exists"
}

@test "new-filter passes doctor checks" {
  shimsumm new-filter mytool
  export PATH="$FILTERS_DIR:$PATH"
  run shimsumm doctor -v
  assert_success
  assert_line "mytool: OK: executable"
  assert_line "mytool: OK: shebang present"
  assert_line "mytool: OK: sources shimsumm emit-wrap"
  assert_line "mytool: OK: calls smsm_wrap"
  assert_line "mytool: OK: syntax ok"
  assert_line "mytool: OK: sources cleanly"
}

@test "new-filter requires an argument" {
  run shimsumm new-filter
  assert_failure
}
