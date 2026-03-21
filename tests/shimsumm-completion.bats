#!/usr/bin/env bats
load 'test_helper'

setup() {
  TEST_TMP=$(mktemp -d)
  export XDG_CONFIG_HOME="$TEST_TMP/config"
  FILTERS_DIR="$XDG_CONFIG_HOME/shimsumm/filters"
  mkdir -p "$FILTERS_DIR"

  printf '#!/bin/sh\n' > "$FILTERS_DIR/git"
  chmod +x "$FILTERS_DIR/git"
  printf '#!/bin/sh\n' > "$FILTERS_DIR/kubectl"
  chmod +x "$FILTERS_DIR/kubectl"
  # Non-executable file — should never appear in completions
  printf '#!/bin/sh\n' > "$FILTERS_DIR/readme"

  export PATH="$PROJECT_ROOT/bin:$PATH"
}

teardown() {
  rm -rf "$TEST_TMP"
}

# Cobra's __complete subcommand prints one candidate per line, then a
# `:N` directive line (4 = ShellCompDirectiveNoFileComp).

@test "test run: completes filter names" {
  run shimsumm __complete test run ''
  assert_success
  assert_output --partial 'git'
  assert_output --partial 'kubectl'
}

@test "test run: does not complete non-executable files" {
  run shimsumm __complete test run ''
  assert_success
  refute_output --partial 'readme'
}

@test "test list: completes filter names" {
  run shimsumm __complete test list ''
  assert_success
  assert_output --partial 'git'
  assert_output --partial 'kubectl'
}

@test "test prompt: completes filter names" {
  run shimsumm __complete test prompt ''
  assert_success
  assert_output --partial 'git'
  assert_output --partial 'kubectl'
}

@test "test add arg0: completes filter names" {
  run shimsumm __complete test add ''
  assert_success
  assert_output --partial 'git'
  assert_output --partial 'kubectl'
}

@test "test add arg1: no completions (case name is new)" {
  run shimsumm __complete test add git ''
  assert_success
  refute_output --partial 'git'
  refute_output --partial 'kubectl'
}

@test "dispatch arg0: completes filter names" {
  run shimsumm __complete dispatch ''
  assert_success
  assert_output --partial 'git'
  assert_output --partial 'kubectl'
}

@test "dispatch arg1: falls back to default (no NoFileComp directive)" {
  run shimsumm __complete dispatch git ''
  assert_success
  # Directive :0 means ShellCompDirectiveDefault — shell file completion allowed
  assert_output --partial ':0'
}

@test "completion directive is NoFileComp for filter-name positions" {
  run shimsumm __complete test run ''
  assert_success
  assert_output --partial ':4'
}
