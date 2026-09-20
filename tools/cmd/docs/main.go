// Command docs builds llms.txt and llms-full.txt from the repository documents. The
// build is deterministic: the same tree gives the same bytes, so a second run changes
// nothing.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jeremygprawira/wlog/tools/internal/workspace"
)

// indexName and fullName are the two files the build writes.
const (
	indexName = "llms.txt"
	fullName  = "llms-full.txt"
)

// main builds both files under the workspace root.
func main() {
	dir := flag.String("dir", "", "the workspace root, which defaults to the directory above tools")
	flag.Parse()

	root := *dir
	if root == "" {
		wd, err := os.Getwd()
		if err != nil {
			fail(err)
		}
		root, err = workspace.FindRoot(wd)
		if err != nil {
			fail(err)
		}
	}
	if err := build(root); err != nil {
		fail(err)
	}
}

// fail prints one error and exits 1.
func fail(err error) {
	fmt.Fprintln(os.Stderr, "docs:", err)
	os.Exit(1)
}

// page is one document of the build.
type page struct {
	path    string // relative to the root, such as docs/event-shape.md
	title   string // the first heading, or the file name
	summary string // the first prose line, for the index
	body    string
}

// build writes the index and the full text.
func build(root string) error {
	pages, err := collect(root)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(root, indexName), []byte(index(pages)), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, fullName), []byte(full(pages)), 0o644)
}

// collect reads every page of the build, in a fixed order.
func collect(root string) ([]page, error) {
	names := []string{"README.md"}
	for _, pattern := range []string{filepath.Join("docs", "*.md"), filepath.Join("docs", "recipes", "*.md")} {
		matches, err := filepath.Glob(filepath.Join(root, pattern))
		if err != nil {
			return nil, err
		}
		sort.Strings(matches)
		for _, match := range matches {
			relative, err := filepath.Rel(root, match)
			if err != nil {
				return nil, err
			}
			names = append(names, filepath.ToSlash(relative))
		}
	}

	pages := make([]page, 0, len(names))
	for _, name := range names {
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			return nil, err
		}
		title, summary := describe(string(body))
		pages = append(pages, page{path: name, title: title, summary: summary, body: string(body)})
	}
	return pages, nil
}

// describe reads the first heading and the first prose line of one document.
func describe(body string) (title, summary string) {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			if title == "" {
				title = strings.TrimSpace(strings.TrimLeft(line, "#"))
			}
			continue
		}
		if summary == "" && !strings.HasPrefix(line, ">") {
			summary = line
		}
		if title != "" && summary != "" {
			break
		}
	}
	return title, summary
}

// index builds the short index: a title, a summary of the project, and one line per
// page.
func index(pages []page) string {
	var out bytes.Buffer
	out.WriteString("# wlog\n\n")
	if len(pages) > 0 {
		out.WriteString("> " + pages[0].summary + "\n\n")
	}
	out.WriteString("## Docs\n\n")
	for _, p := range pages {
		fmt.Fprintf(&out, "- [%s](%s): %s\n", p.title, p.path, p.summary)
	}
	return out.String()
}

// full builds the full text: every page under its own heading.
func full(pages []page) string {
	var out bytes.Buffer
	out.WriteString("# wlog, full documentation\n\n")
	for _, p := range pages {
		out.WriteString("---\n\n")
		fmt.Fprintf(&out, "# %s (%s)\n\n", p.title, p.path)
		out.WriteString(strings.TrimRight(p.body, "\n"))
		out.WriteString("\n\n")
	}
	return out.String()
}
