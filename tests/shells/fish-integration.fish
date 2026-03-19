#!/usr/bin/env fish
# Native fish integration tests for shimsumm init.
# Tests that shimsumm init fish | source in a real fish subprocess behaves correctly.

set PROJECT_ROOT (cd (dirname (status filename))/../.. && pwd)
set BIN "$PROJECT_ROOT/bin/shimsumm"

if not test -x "$BIN"
    printf 'ERROR: shimsumm not found at %s\n' "$BIN" >&2; exit 1
end

set _PASS 0; set _FAIL 0
function _pass; printf 'PASS: %s\n' $argv[1]; set -g _PASS (math $_PASS + 1); end
function _fail; printf 'FAIL: %s\n' $argv[1]; set -g _FAIL (math $_FAIL + 1); end

set TMP (mktemp -d)
function _cleanup; rm -rf $TMP; end
trap _cleanup EXIT

set XDG "$TMP/xdg"
set HOME2 "$TMP/home2"
set OUTER_PATH "$PROJECT_ROOT/bin:$PATH"
mkdir -p "$XDG/shimsumm/filters" "$HOME2/.config/shimsumm/filters"

# 1. prepends filters dir to PATH
set actual (fish -c "
  set -x XDG_CONFIG_HOME '$XDG'
  set -x PATH (string split ':' '$OUTER_PATH')
  '$BIN' init fish | source
  printf '%s\n' \$PATH[1]
")
if test "$actual" = "$XDG/shimsumm/filters"
    _pass "init prepends filters dir to PATH"
else
    _fail "init prepends filters dir to PATH"
end

# 2. idempotent
set count (fish -c "
  set -x XDG_CONFIG_HOME '$XDG'
  set -x PATH (string split ':' '$OUTER_PATH')
  '$BIN' init fish | source
  '$BIN' init fish | source
  printf '%s\n' \$PATH | grep -cxF '$XDG/shimsumm/filters'
")
if test "$count" = "1"
    _pass "init is idempotent"
else
    _fail "init is idempotent"
end

# 3. XDG_CONFIG_HOME override
set CUSTOM "$TMP/custom"
mkdir -p "$CUSTOM/shimsumm/filters"
set actual (fish -c "
  set -x XDG_CONFIG_HOME '$CUSTOM'
  set -x PATH (string split ':' '$OUTER_PATH')
  '$BIN' init fish | source
  printf '%s\n' \$PATH[1]
")
if test "$actual" = "$CUSTOM/shimsumm/filters"
    _pass "init respects XDG_CONFIG_HOME"
else
    _fail "init respects XDG_CONFIG_HOME"
end

# 4. HOME/.config fallback
set actual (fish -c "
  set -e XDG_CONFIG_HOME
  set -x HOME '$HOME2'
  set -x PATH (string split ':' '$OUTER_PATH')
  '$BIN' init fish | source
  printf '%s\n' \$PATH[1]
")
if test "$actual" = "$HOME2/.config/shimsumm/filters"
    _pass "init falls back to HOME/.config"
else
    _fail "init falls back to HOME/.config"
end

# 5. no variable leak
set leaked (fish -c "
  set -x XDG_CONFIG_HOME '$XDG'
  set -x PATH (string split ':' '$OUTER_PATH')
  '$BIN' init fish | source
  if set -q _smsm_f; printf 'leaked\n'; else; printf 'clean\n'; end
")
if test "$leaked" = "clean"
    _pass "init leaves no _smsm_* variables"
else
    _fail "init leaves no _smsm_* variables"
end

# 6. full flow: init -> filter on PATH -> shimsumm-wrap -> real tool -> filtered output
set REAL "$TMP/real"
mkdir -p "$REAL"
printf '#!/bin/sh\nprintf "KEEP this\nDROP this\n"\n' > "$REAL/mytool"
chmod +x "$REAL/mytool"
printf '#!/bin/sh\neval "$(shimsumm emit-wrap)"\nsmsm_filter() { grep KEEP || true; }\nsmsm_wrap "$@"\n' \
  > "$XDG/shimsumm/filters/mytool"
chmod +x "$XDG/shimsumm/filters/mytool"

set actual (fish -c "
  set -x XDG_CONFIG_HOME '$XDG'
  set -x PATH (string split ':' '$OUTER_PATH')
  '$BIN' init fish | source
  set -x PATH \$PATH '$REAL'
  mytool 2>&1 | grep -v '^\[full output:'
")
if test "$actual" = "KEEP this"
    _pass "full flow: filter intercepts and transforms output"
else
    _fail "full flow: filter intercepts and transforms output"
end

printf '\n%d passed, %d failed\n' $_PASS $_FAIL
test $_FAIL -eq 0
