//go:build tools

// Package deptools keeps the developer commands in the build list of this module.
//
// The commands run through `go run`, so no Go file imports them. go mod tidy
// reads this file with every build tag enabled, so the modules stay required and
// a run needs no network for the first download. The package never compiles,
// because the tools tag is off.
package deptools

import (
	_ "golang.org/x/perf/cmd/benchstat"
	_ "golang.org/x/vuln/cmd/govulncheck"
)
