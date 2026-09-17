package entry_test

import (
	"sort"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/jeremygprawira/wlog/cmd/wlog/entry"
)

// TestMap_CLI8_GroupPrefixes proves a route keeps the prefix of the Echo, Gin, or mux group it
// was registered on, and that a route written as a constant resolves.
func TestMap_CLI8_GroupPrefixes(t *testing.T) {
	pkgs, err := entry.Load("./../testdata/routes_app")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	found := map[string]bool{}
	for _, point := range entry.Find(pkgs) {
		found[point.Method+" "+point.Route] = true
	}

	want := []string{
		"DELETE /admin/users/:id/role", // Echo group prefix, handler before its middleware
		"POST /api/orders/:id",         // Gin group prefix
		"DELETE /admin/logs",           // mux subrouter prefix, first method
		"POST /admin/logs",             // mux subrouter prefix, second method
		" /health",                     // a route written as a constant
	}
	for _, key := range want {
		if !found[key] {
			t.Errorf("no route %q; found:\n%s", key, routesFound(pkgs))
		}
	}
}

// TestMap_CLI8_MuxMethods proves a mux chain that names several methods gives one entry per
// method, so a write route is never scored as a read route.
func TestMap_CLI8_MuxMethods(t *testing.T) {
	pkgs, err := entry.Load("./../testdata/routes_app")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	methods := map[string]bool{}
	for _, point := range entry.Find(pkgs) {
		if point.Route == "/admin/logs" {
			methods[point.Method] = true
		}
	}
	for _, method := range []string{"DELETE", "POST"} {
		if !methods[method] {
			t.Errorf("the /admin/logs chain gave no %s entry: %v", method, methods)
		}
	}
}

// routesFound renders every route the map found, so a failure shows what it saw.
func routesFound(pkgs []*packages.Package) string {
	lines := []string{}
	for _, point := range entry.Find(pkgs) {
		lines = append(lines, "  "+point.Method+" "+point.Route+" -> "+point.Package+"."+point.Function)
	}
	sort.Strings(lines)
	out := ""
	for _, line := range lines {
		out += line + "\n"
	}
	return out
}
