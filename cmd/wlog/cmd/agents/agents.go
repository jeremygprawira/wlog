// Package agents implements `wlog agents`: it writes a wlog block into AGENTS.md and
// three skill files from the repository's own guidance. A rerun replaces only the fenced
// block, so a file it did not write is never touched.
package agents

import (
	"embed"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// templateFiles holds the guidance, kept in step with docs/ by the golden test.
//
//go:embed templates/*.md
var templateFiles embed.FS

// skillFiles are the three skills agents writes, in order.
var skillFiles = []struct {
	file     string
	template string
}{
	{"instrument-with-wlog.md", "templates/instrument-with-wlog.md"},
	{"audit-a-handler.md", "templates/audit-a-handler.md"},
	{"analyze-wlog-output.md", "templates/analyze-wlog-output.md"},
}

// blockStart and blockEnd fence the AGENTS.md block.
const (
	blockStart = "<!-- wlog:start -->"
	blockEnd   = "<!-- wlog:end -->"
)

// Run parses args and writes the block and skills, returning the process exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("agents", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dir := flags.String("dir", ".", "the project directory")
	skillsDir := flags.String("skills-dir", ".agents/skills", "where to write the skills")
	agentsMD := flags.String("agents-md", "AGENTS.md", "the file that holds the block")
	dryRun := flags.Bool("dry-run", false, "print the plan and write nothing")
	if err := flags.Parse(args); err != nil {
		return 2
	}

	writes, err := plan(*dir, *skillsDir, *agentsMD)
	if err != nil {
		fmt.Fprintln(stderr, "wlog agents:", err)
		return 1
	}
	if *dryRun {
		for _, write := range writes {
			fmt.Fprintf(stdout, "--- %s\n%s\n", write.path, write.content)
		}
		return 0
	}
	for _, write := range writes {
		if err := os.MkdirAll(filepath.Dir(write.path), 0o755); err != nil {
			fmt.Fprintln(stderr, "wlog agents:", err)
			return 1
		}
		if err := os.WriteFile(write.path, []byte(write.content), 0o644); err != nil {
			fmt.Fprintln(stderr, "wlog agents:", err)
			return 1
		}
		fmt.Fprintln(stdout, "wrote", write.path)
	}
	return 0
}

// write is one planned file.
type write struct {
	path    string
	content string
}

// plan builds every file change before anything is written.
func plan(dir, skillsDir, agentsMD string) ([]write, error) {
	block, err := templateFiles.ReadFile("templates/agents-block.md")
	if err != nil {
		return nil, err
	}
	agentsPath := filepath.Join(dir, agentsMD)
	existing, _ := os.ReadFile(agentsPath)
	writes := []write{{
		path:    agentsPath,
		content: mergeBlock(string(existing), string(block)),
	}}
	for _, skill := range skillFiles {
		content, err := templateFiles.ReadFile(skill.template)
		if err != nil {
			return nil, err
		}
		writes = append(writes, write{
			path:    filepath.Join(dir, skillsDir, skill.file),
			content: string(content),
		})
	}
	return writes, nil
}

// mergeBlock replaces the fenced block in existing, or appends one. Content outside the
// fences is kept exactly as it was.
func mergeBlock(existing, block string) string {
	fenced := blockStart + "\n" + strings.TrimRight(block, "\n") + "\n" + blockEnd + "\n"
	start := strings.Index(existing, blockStart)
	end := strings.Index(existing, blockEnd)
	if start == -1 || end == -1 || end < start {
		if existing == "" {
			return fenced
		}
		return strings.TrimRight(existing, "\n") + "\n\n" + fenced
	}
	end += len(blockEnd)
	if end < len(existing) && existing[end] == '\n' {
		end++
	}
	return existing[:start] + fenced + existing[end:]
}
