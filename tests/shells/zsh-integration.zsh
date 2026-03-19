#!/bin/zsh
# Native zsh integration tests for shimsumm init.
# Tests that source =(shimsumm init) in a real zsh subprocess behaves correctly.
set -uo pipefail

PROJECT_ROOT="${${(%):-%x}:A:h:h:h}"
BIN="$PROJECT_ROOT/bin/shimsumm"

[[ -x "$BIN" ]] || { printf 'ERROR: shimsumm not found at %s\n' "$BIN" >&2; exit 1; }

_PASS=0; _FAIL=0
_pass() { printf 'PASS: %s\n' "$1"; (( ++_PASS )); true; }
_fail() { printf 'FAIL: %s\n' "$1"; (( ++_FAIL )); true; }

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

XDG="$TMP/xdg"
HOME2="$TMP/home2"
OUTER_PATH="$PROJECT_ROOT/bin:$PATH"
mkdir -p "$XDG/shimsumm/filters" "$HOME2/.config/shimsumm/filters"

# 1. prepends filters dir to PATH
actual=$(zsh -c "
  export XDG_CONFIG_HOME='$XDG'
  export PATH='$OUTER_PATH'
  source =('$BIN' init)
  printf '%s\n' \"\${PATH%%:*}\"
")
[[ "$actual" == "$XDG/shimsumm/filters" ]] \
  && _pass "init prepends filters dir to PATH" \
  || _fail "init prepends filters dir to PATH"

# 2. idempotent
count=$(zsh -c "
  export XDG_CONFIG_HOME='$XDG'
  export PATH='$OUTER_PATH'
  source =('$BIN' init)
  source =('$BIN' init)
  printf '%s\n' \"\$PATH\" | tr ':' '\n' | grep -cxF '$XDG/shimsumm/filters'
")
[[ "$count" == "1" ]] \
  && _pass "init is idempotent" \
  || _fail "init is idempotent"

# 3. XDG_CONFIG_HOME override
CUSTOM="$TMP/custom"
mkdir -p "$CUSTOM/shimsumm/filters"
actual=$(zsh -c "
  export XDG_CONFIG_HOME='$CUSTOM'
  export PATH='$OUTER_PATH'
  source =('$BIN' init)
  printf '%s\n' \"\${PATH%%:*}\"
")
[[ "$actual" == "$CUSTOM/shimsumm/filters" ]] \
  && _pass "init respects XDG_CONFIG_HOME" \
  || _fail "init respects XDG_CONFIG_HOME"

# 4. HOME/.config fallback
actual=$(zsh -c "
  unset XDG_CONFIG_HOME
  export HOME='$HOME2'
  export PATH='$OUTER_PATH'
  source =('$BIN' init)
  printf '%s\n' \"\${PATH%%:*}\"
")
[[ "$actual" == "$HOME2/.config/shimsumm/filters" ]] \
  && _pass "init falls back to HOME/.config" \
  || _fail "init falls back to HOME/.config"

# 5. no variable leak
leaked=$(zsh -c "
  export XDG_CONFIG_HOME='$XDG'
  export PATH='$OUTER_PATH'
  source =('$BIN' init)
  [[ -n \"\${_smsm_filters+set}\" ]] && printf 'leaked\n' || printf 'clean\n'
")
[[ "$leaked" == "clean" ]] \
  && _pass "init leaves no _smsm_* variables" \
  || _fail "init leaves no _smsm_* variables"

# 6. full flow: init -> filter on PATH -> shimsumm-wrap -> real tool -> filtered output
REAL="$TMP/real"
mkdir -p "$REAL"
cat > "$REAL/mytool" <<'SH'
#!/bin/sh
printf 'KEEP this\nDROP this\n'
SH
chmod +x "$REAL/mytool"
cat > "$XDG/shimsumm/filters/mytool" <<SH
#!/bin/sh
eval "\$(shimsumm wrap)"
smsm_filter() { grep KEEP || true; }
smsm_wrap "\$@"
SH
chmod +x "$XDG/shimsumm/filters/mytool"

actual=$(zsh -c "
  export XDG_CONFIG_HOME='$XDG'
  export PATH='$OUTER_PATH'
  source =('$BIN' init)
  export PATH=\"\$PATH:$REAL\"
  mytool 2>&1 | grep -v '^\[full output:'
")
[[ "$actual" == "KEEP this" ]] \
  && _pass "full flow: filter intercepts and transforms output" \
  || _fail "full flow: filter intercepts and transforms output"

printf '\n%d passed, %d failed\n' "$_PASS" "$_FAIL"
[ "$_FAIL" -eq 0 ]
