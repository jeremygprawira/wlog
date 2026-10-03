package init

import (
	"strings"
	"testing"
)

// TestInit_I3_CommentStaysOutsideTheCall proves a comment between the router and the
// next statement stays outside the inserted Use call.
func TestInit_I3_CommentStaysOutsideTheCall(t *testing.T) {
	source := []byte(`package main

import "github.com/labstack/echo/v4"

func main() {
	e := echo.New()
	// Routes
	e.GET("/", nil)
}
`)
	out, changed, err := rewrite("main.go", source, "echo")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("rewrite changed nothing")
	}
	text := string(out)
	if !strings.Contains(text, "e.Use(LoggerMiddleware())") {
		t.Fatalf("the Use call was split:\n%s", text)
	}
	useAt := strings.Index(text, "e.Use(LoggerMiddleware())")
	commentAt := strings.Index(text, "// Routes")
	getAt := strings.Index(text, "e.GET")
	if useAt < 0 || commentAt < 0 || getAt < 0 || useAt >= commentAt || commentAt >= getAt {
		t.Fatalf("comment moved into the call:\n%s", text)
	}
}
