package rules

import (
	"golang.org/x/tools/go/packages"

	"github.com/jeremygprawira/wlog/cmd/wlog/entry"
)

// noPrint passes when the handler does not log through fmt or the log package. A
// handler should put its data on the event, not on stdout or the standard logger.
func noPrint(pkg *packages.Package, point entry.Point) Check {
	if bodyCalls(pkg, point, isPrintCall) {
		return fail(RuleNoPrint, WeightNoPrint, "handler uses print logging")
	}
	return pass(RuleNoPrint, WeightNoPrint)
}

// isPrintCall reports whether a call writes to stdout or the standard logger.
func isPrintCall(pkgPath, name string) bool {
	switch pkgPath {
	case "fmt":
		switch name {
		case "Print", "Printf", "Println", "Fprint", "Fprintf", "Fprintln":
			return true
		}
	case "log":
		switch name {
		case "Print", "Printf", "Println", "Fatal", "Fatalf", "Fatalln", "Panic", "Panicf", "Panicln":
			return true
		}
	}
	return false
}
