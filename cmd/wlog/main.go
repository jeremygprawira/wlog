package main

import (
	"fmt"
	"os"

	"github.com/jeremygprawira/wlog/cmd/wlog/entry"
)

// main is a placeholder until MP5 wires the full map command.
func main() {
	pkgs, err := entry.Load(os.Args[1:]...)
	if err != nil {
		fmt.Fprintln(os.Stderr, "wlog: load:", err)
		os.Exit(2)
	}
	for _, point := range entry.Find(pkgs) {
		fmt.Printf("%s %s %s:%d %s %s\n", point.Package, point.Function, point.File, point.Line, point.Method, point.Route)
	}
}
