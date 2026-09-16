package entry_test

import (
	"net/http"
	"testing"

	"github.com/jeremygprawira/wlog/cmd/wlog/entry"
)

// loadFixtures loads the named fixture apps and fails the test on any load error.
func loadFixtures(t *testing.T, names ...string) []entry.Point {
	t.Helper()
	patterns := make([]string, 0, len(names))
	for _, name := range names {
		patterns = append(patterns, "../testdata/"+name)
	}
	pkgs, err := entry.Load(patterns...)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, pkg := range pkgs {
		for _, pkgErr := range pkg.Errors {
			t.Errorf("load error in %s: %v", pkg.PkgPath, pkgErr)
		}
	}
	return entry.Find(pkgs)
}

// pointFor returns the first point with the given function name.
func pointFor(points []entry.Point, function string) *entry.Point {
	for i := range points {
		if points[i].Function == function {
			return &points[i]
		}
	}
	return nil
}

// TestFind_NetHTTP proves a named handler and a function literal are both found, with
// their route and method.
func TestFind_NetHTTP(t *testing.T) {
	points := loadFixtures(t, "nethttp_app")
	if len(points) != 2 {
		t.Fatalf("found %d points, want 2: %+v", len(points), points)
	}

	named := pointFor(points, "handleOrders")
	if named == nil {
		t.Fatal("handleOrders not found")
	}
	if named.Framework != "net/http" || named.Method != http.MethodGet || named.Route != "/orders/{id}" {
		t.Errorf("handleOrders = %s %s, want GET /orders/{id}", named.Method, named.Route)
	}
	if named.File != "main.go" || named.Line == 0 {
		t.Errorf("handleOrders position = %s:%d, want main.go with a line", named.File, named.Line)
	}

	literal := pointFor(points, "main")
	if literal == nil {
		t.Fatal("the function literal in main not found")
	}
	if literal.Method != "" || literal.Route != "/health" {
		t.Errorf("literal = %q %q, want an unmethoded /health", literal.Method, literal.Route)
	}
}

// TestFind_Mux proves a mux registration is found with the method from the chained
// .Methods call.
func TestFind_Mux(t *testing.T) {
	points := loadFixtures(t, "mux_app")
	if len(points) != 1 {
		t.Fatalf("found %d points, want 1: %+v", len(points), points)
	}
	point := points[0]
	if point.Framework != "github.com/gorilla/mux" || point.Method != http.MethodGet || point.Route != "/orders/{id}" {
		t.Errorf("point = %s %s %s, want mux GET /orders/{id}", point.Framework, point.Method, point.Route)
	}
	if point.Function != "main" {
		t.Errorf("Function = %q, want main for the literal", point.Function)
	}
}
