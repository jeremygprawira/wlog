// Command ste runs the Simple English lint over markdown files and Go doc
// comments.
//
// The rules and their names live in tools/internal/ste. This command finds the
// text to check, prints one line per hit as path:line: code: message, and exits
// 1 on any hit.
// the older specs from blocking CI while the documentation tasks rewrite them.
//
// Without arguments the command checks the markdown files of the repository.
// With -comments it also checks the doc comment of every exported declaration in
// every module of the workspace. The documentation tasks turn that flag on for
// good once the doc comments follow the rules.
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jeremygprawira/wlog/tools/internal/ste"
	"github.com/jeremygprawira/wlog/tools/internal/workspace"
)

// main finds the workspace root and exits 1 when any file fails.
func main() {
	register := flag.String("type", string(ste.Descriptive), "descriptive or procedural")
	comments := flag.Bool("comments", false, "also check the Go doc comments")
	flag.Parse()

	wd, err := os.Getwd()
	if err != nil {
		fail(err)
	}
	root, err := workspace.FindRoot(wd)
	if err != nil {
		fail(err)
	}
	if err := run(root, flag.Args(), ste.Register(*register), *comments, os.Stdout); err != nil {
		fail(err)
	}
}

// fail prints the error on stderr and exits 1.
func fail(err error) {
	fmt.Fprintln(os.Stderr, "ste:", err)
	os.Exit(1)
}

// passage is one piece of text to check, with the line it starts on.
type passage struct {
	file  string // path relative to the root, with a leading "./"
	line  int    // the line of the first line of text in the file
	text  string
	label string // the declaration a doc comment belongs to, empty for markdown
}

// run checks every target and prints one line per hit.
func run(root string, args []string, register ste.Register, comments bool, out io.Writer) error {
	passages, err := collect(root, args, comments)
	if err != nil {
		return err
	}

	var problems []string
	bad := 0
	for _, p := range passages {
		for _, hit := range ste.Lint(p.text, register).Hits {
			bad++
			problems = append(problems, fmt.Sprintf("%s:%d: %s: %s",
				p.file, p.line+hit.Line-1, code(hit.Category), squeeze(message(hit))))
		}
	}
	sort.Strings(problems)
	for _, line := range problems {
		if _, err := fmt.Fprintln(out, line); err != nil {
			return err
		}
	}
	if bad > 0 {
		return fmt.Errorf("%d hit(s) in %s text", bad, register)
	}
	return nil
}

// collect returns every passage to check. With arguments it reads those paths,
// which may be a markdown file or a directory. Without arguments it walks the
// repository.
func collect(root string, args []string, comments bool) ([]passage, error) {
	if len(args) > 0 {
		var out []passage
		for _, arg := range args {
			path := arg
			if !filepath.IsAbs(path) {
				path = filepath.Join(root, arg)
			}
			info, err := os.Stat(path)
			if err != nil {
				return nil, err
			}
			if info.IsDir() {
				found, err := walkDir(root, path, comments)
				if err != nil {
					return nil, err
				}
				out = append(out, found...)
				continue
			}
			if comments && strings.HasSuffix(path, ".go") {
				found, err := goDocComments(root, path)
				if err != nil {
					return nil, err
				}
				out = append(out, found...)
				continue
			}
			out = append(out, markdown(root, path))
		}
		return out, nil
	}
	return walkDir(root, root, comments)
}

// walkDir returns every markdown file and every Go doc comment under dir.
//
// It skips vendor, testdata, and hidden directories, the tools module, because
// the fixtures of a tool hold text that is bad on purpose, and tasks/, because
// that directory holds the working notes of the maintainer and not a document
// for a user.
func walkDir(root, dir string, comments bool) ([]passage, error) {
	var out []passage
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if path != dir && (name == "vendor" || name == "testdata" || name == "tasks" || strings.HasPrefix(name, ".") || name == "tools") {
				return fs.SkipDir
			}
			return nil
		}
		switch {
		case strings.HasSuffix(path, ".md"):
			out = append(out, markdown(root, path))
		case comments && strings.HasSuffix(path, ".go"):
			found, err := goDocComments(root, path)
			if err != nil {
				return err
			}
			out = append(out, found...)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// markdown returns one passage for a markdown file.
func markdown(root, path string) passage {
	data, err := os.ReadFile(path)
	if err != nil {
		return passage{file: rel(root, path), line: 1, text: ""}
	}
	return passage{file: rel(root, path), line: 1, text: string(data)}
}

// squeeze joins the lines of a message into one line, so a report holds one line
// per hit. A long text is cut at 80 characters.
func squeeze(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if len(text) <= 80 {
		return text
	}
	return text[:80] + "..."
}

// goDocComments returns one passage per doc comment of the exported declarations
// in a Go file.
//
// The text of an ast.CommentGroup keeps its own line numbers, so a hit inside a
// comment maps back to the file with the line of the first comment line.
func goDocComments(root, path string) ([]passage, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	var out []passage
	add := func(doc *ast.CommentGroup, label string) {
		if doc == nil {
			return
		}
		text := doc.Text()
		if strings.TrimSpace(text) == "" {
			return
		}
		out = append(out, passage{
			file:  rel(root, path),
			line:  fset.Position(doc.Pos()).Line,
			text:  text,
			label: label,
		})
	}

	add(file.Doc, "package "+file.Name.Name)
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					add(s.Doc, s.Name.Name)
				case *ast.ValueSpec:
					label := ""
					if len(s.Names) > 0 {
						label = s.Names[0].Name
					}
					add(s.Doc, label)
				}
			}
		case *ast.FuncDecl:
			name := d.Name.Name
			if d.Recv != nil && len(d.Recv.List) > 0 {
				name = "method " + name
			}
			add(d.Doc, name)
		}
	}
	return out, nil
}

// code turns a category into the code that a report prints.
func code(category string) string { return strings.ToUpper(category) }

// message explains one hit in the words of the rule.
func message(hit ste.Hit) string {
	switch hit.Category {
	case "sentence_over_limit":
		return "the sentence is too long: " + hit.Text
	case "trailing_condition":
		return "name the condition before the command: " + hit.Text
	case "contraction":
		return "spell it out: " + hit.Text
	case "banned_modal":
		return "use can, will, or must: " + hit.Text
	case "perfect_tense":
		return "use a simple tense: " + hit.Text
	case "ing_clause":
		return "write a second sentence: " + hit.Text
	case "semicolon":
		return "write two sentences"
	case "em_dash":
		return "drop the dash and name the relation: " + hit.Text
	case "latin_abbrev":
		return "write for example, that is, or and so on: " + hit.Text
	case "slop_word":
		return "name the measured fact instead: " + hit.Text
	case "synonym_rotation":
		return "one word, one meaning: " + hit.Text
	}
	return hit.Text
}

// rel returns a path relative to the root, in the "./x" form that the known-bad
// list uses.
func rel(root, path string) string { return "./" + filepath.ToSlash(mustRel(root, path)) }

// mustRel returns a path relative to the root, or the path itself.
func mustRel(root, path string) string {
	r, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return r
}
