package main

import (
	"strings"
	"testing"
)

// TestWlog_I19_HelpListsEveryCommand proves help names each command the binary runs.
func TestWlog_I19_HelpListsEveryCommand(t *testing.T) {
	text := usage()
	for _, name := range []string{
		"map", "init", "doctor", "agents", "query", "tail", "mcp",
		"explain", "rules", "schema", "version", "env", "help",
	} {
		if !strings.Contains(text, "\n  "+name+" ") && !strings.Contains(text, "\n  "+name+"\t") {
			t.Errorf("help does not list %s", name)
		}
	}
}
