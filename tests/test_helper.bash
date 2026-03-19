load '/opt/bats/support/load'
load '/opt/bats/assert/load'

bats_require_minimum_version 1.5.0

export PROJECT_ROOT="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"
