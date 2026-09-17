package entry_test

import (
	"sort"
	"testing"

	"github.com/jeremygprawira/wlog/cmd/wlog/entry"
)

// TestMap_CLI1_CrossPackageHandlers proves discovery follows a handler to the code that runs,
// wherever that code lives: another package, a conversion, a closure factory, a type with
// ServeHTTP, and the chained or list-based registrations of mux, Echo, and Gin.
func TestMap_CLI1_CrossPackageHandlers(t *testing.T) {
	pkgs, err := entry.Load("./../testdata/crosspkg")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	points := entry.Find(pkgs)
	if len(points) == 0 {
		t.Fatal("Find returned no handlers for the crosspkg fixture")
	}

	// Every handler the fixture registers must appear, named for what will run.
	type want struct {
		pkg      string
		function string
		method   string
		route    string
	}
	const handlerPkg = "github.com/jeremygprawira/wlog/cmd/wlog/testdata/crosspkg/handler"
	wants := []want{
		{handlerPkg, "Orders", "GET", "/orders/{id}"},   // net/http, another package
		{handlerPkg, "Orders", "GET", "/audit"},         // http.HandlerFunc(handler.Orders)
		{handlerPkg, "Refund", "POST", "/refunds/{id}"}, // mux chain, factory call
		{handlerPkg, "ServeHTTP", "", "/audit-mux"},     // a type with ServeHTTP
		{handlerPkg, "Orders", "", "/echo/orders/{id}"}, // Echo Match
		{handlerPkg, "Refund", "POST", "/echo/refunds"}, // Echo Add
		{handlerPkg, "Orders", "", "/gin/orders"},       // Gin Match
	}

	found := map[string]bool{}
	for _, point := range points {
		key := point.Package + "|" + point.Function + "|" + point.Method + "|" + point.Route
		found[key] = true
	}
	for _, w := range wants {
		key := w.pkg + "|" + w.function + "|" + w.method + "|" + w.route
		if !found[key] {
			t.Errorf("no handler %s %s -> %s in %s; found:\n%s", w.method, w.route, w.function, w.pkg, describe(points))
		}
	}
}

// describe renders the found handlers, so a failure shows what discovery did see.
func describe(points []entry.Point) string {
	lines := make([]string, 0, len(points))
	for _, point := range points {
		lines = append(lines, point.Package+" "+point.Method+" "+point.Route+" -> "+point.Function)
	}
	sort.Strings(lines)
	out := ""
	for _, line := range lines {
		out += "  " + line + "\n"
	}
	return out
}
