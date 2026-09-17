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

// skillMarker marks a file this command wrote. A skill file without it belongs to the caller, and
// is never overwritten.
const skillMarker = "<!-- wlog:skill -->"

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
		_, _ = fmt.Fprintln(stderr, "wlog agents:", err)
		return 1
	}
	if *dryRun {
		for _, write := range writes {
			_, _ = fmt.Fprintf(stdout, "--- %s\n%s\n", write.path, write.content)
		}
		return 0
	}
	for _, write := range writes {
		if err := os.MkdirAll(filepath.Dir(write.path), 0o755); err != nil {
			_, _ = fmt.Fprintln(stderr, "wlog agents:", err)
			return 1
		}
		if err := os.WriteFile(write.path, []byte(write.content), 0o644); err != nil {
			_, _ = fmt.Fprintln(stderr, "wlog agents:", err)
			return 1
		}
		_, _ = fmt.Fprintln(stdout, "wrote", write.path)
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
	merged, err := mergeBlock(string(existing), string(block))
	if err != nil {
		return nil, err
	}
	writes := []write{{path: agentsPath, content: merged}}

	for _, skill := range skillFiles {
		content, err := templateFiles.ReadFile(skill.template)
		if err != nil {
			return nil, err
		}
		path := filepath.Join(dir, skillsDir, skill.file)
		if edited := existingSkill(path); edited != "" {
			// The caller edited this file, and their work outranks ours.
			return nil, fmt.Errorf("%s carries no %s marker, so it was not written by wlog agents: move it aside or delete it", path, skillMarker)
		}
		writes = append(writes, write{path: path, content: string(content)})
	}
	return writes, nil
}

// existingSkill returns the content of a skill file a caller already has, or "" when the file is
// absent or carries the marker.
func existingSkill(path string) string {
	content, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	if strings.Contains(string(content), skillMarker) {
		return ""
	}
	return string(content)
}

// mergeBlock replaces the fenced block in existing, or appends one. Content outside the fences is
// kept exactly as it was, including its line endings.
//
// A fence on its own is refused with the line number of the fence that is there: the text between
// a start fence and the end of the file belongs to the caller, and a tool that treated it as its
// own would delete it on the next run.
func mergeBlock(existing, block string) (string, error) {
	lineEnding := "\n"
	if strings.Contains(existing, "\r\n") {
		lineEnding = "\r\n"
	}
	fenced := blockStart + lineEnding + strings.TrimRight(block, "\n") + lineEnding + blockEnd + lineEnding

	start := strings.Index(existing, blockStart)
	end := strings.Index(existing, blockEnd)
	switch {
	case start == -1 && end == -1:
		if existing == "" {
			return fenced, nil
		}
		return strings.TrimRight(existing, "\n") + lineEnding + lineEnding + fenced, nil
	case start == -1:
		return "", fmt.Errorf("%s at line %d has no %s before it", blockEnd, lineOf(existing, end), blockStart)
	case end == -1 || end < start:
		return "", fmt.Errorf("%s at line %d has no %s after it", blockStart, lineOf(existing, start), blockEnd)
	}
	end += len(blockEnd)
	if end < len(existing) && existing[end] == '\r' {
		end++
	}
	if end < len(existing) && existing[end] == '\n' {
		end++
	}
	return existing[:start] + fenced + existing[end:], nil
}

// lineOf returns the 1-based line number of an offset in text.
func lineOf(text string, offset int) int {
	if offset < 0 || offset > len(text) {
		return 0
	}
	return strings.Count(text[:offset], "\n") + 1
}
