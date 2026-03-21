package main

import (
	_ "embed"
	"strings"
)

//go:embed smsm_wrap.sh
var smsmWrapSh string

func emitSmsmWrap() string {
	return strings.TrimRight(smsmWrapSh, "\n")
}
