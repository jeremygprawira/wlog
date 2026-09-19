// This file holds the follow path of wlog query, which is what wlog tail runs.
package query

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/jeremygprawira/wlog/query"
)

// pollInterval is how long the follow loop waits before it looks for more data.
var pollInterval = 200 * time.Millisecond

// followSources reads the sources once and then keeps reading appended lines until the
// process is interrupted. A stream, a stdin, and a URL have no end to follow, so they are
// read once.
func followSources(sources []string, url string, filter *query.Filter, print func(map[string]any) error, stderr io.Writer) int {
	if url != "" {
		sources = append(sources, url)
	}
	if len(sources) == 0 {
		sources = defaultSources()
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	matches := 0
	counted := func(event map[string]any) error {
		matches++
		return print(event)
	}
	for _, source := range sources {
		if source == "-" {
			if err := scan(os.Stdin, filter, &followWindow{}, func(string) {}); err != nil {
				_, _ = fmt.Fprintf(stderr, "wlog query: %v\n", err)
				return 2
			}
			continue
		}
		if url != "" && source == url {
			if err := readURL(source, filter, &followWindow{print: counted}); err != nil {
				_, _ = fmt.Fprintf(stderr, "wlog query: %v\n", err)
				return 2
			}
			continue
		}
		if err := followPath(ctx, source, filter, counted, stderr); err != nil {
			_, _ = fmt.Fprintf(stderr, "wlog query: %v\n", err)
			return 2
		}
	}
	if matches == 0 {
		return 1
	}
	return 0
}

// followPath follows one file, or every event file of one folder.
func followPath(ctx context.Context, source string, filter *query.Filter, print func(map[string]any) error, stderr io.Writer) error {
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return followFile(ctx, source, filter, print)
	}
	names, err := folderFiles(source)
	if err != nil {
		return err
	}
	for _, name := range names {
		if err := followFile(ctx, name, filter, print); err != nil {
			return err
		}
	}
	return nil
}

// followWindow is the counting window of the follow path: every match prints at once.
type followWindow struct {
	print func(map[string]any) error
}

// add prints one match.
func (w *followWindow) add(event map[string]any) {
	if w.print != nil {
		_ = w.print(event)
	}
}

// full reports false, because the follow path never stops early.
func (w *followWindow) full() bool { return false }

// followFile prints the matches of one file, and it keeps reading appended lines and a
// file that replaced the original, until ctx is done.
func followFile(ctx context.Context, path string, filter *query.Filter, print func(map[string]any) error) error {
	var file *os.File
	var info os.FileInfo
	var reader *bufio.Reader
	var carry []byte

	for {
		if file == nil {
			opened, err := os.Open(path)
			if err != nil {
				if !errors.Is(err, os.ErrNotExist) {
					return err
				}
				if wait(ctx) {
					return nil
				}
				continue
			}
			stat, err := opened.Stat()
			if err != nil {
				_ = opened.Close()
				return err
			}
			file, info, reader = opened, stat, bufio.NewReaderSize(opened, 1<<20)
		}

		eof, err := readAppended(reader, filter, print, &carry)
		if err != nil {
			return err
		}
		if !eof {
			continue
		}
		if stat, err := os.Stat(path); err == nil && !os.SameFile(stat, info) {
			// The file was rotated: read the new file from its own start.
			_ = file.Close()
			file, info, reader, carry = nil, nil, nil, nil
			continue
		}
		if wait(ctx) {
			return nil
		}
	}
}

// readAppended reads the lines the reader holds, and it reports the end of the file. A
// partial last line waits in carry for the rest of the line.
func readAppended(reader *bufio.Reader, filter *query.Filter, print func(map[string]any) error, carry *[]byte) (bool, error) {
	for {
		chunk, err := reader.ReadBytes('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				*carry = append(*carry, chunk...)
				return true, nil
			}
			return false, err
		}
		line := chunk
		if len(*carry) > 0 {
			line = append(*carry, chunk...)
			*carry = (*carry)[:0]
		}
		if err := followLine(line, filter, print); err != nil {
			return false, err
		}
	}
}

// followLine prints one line when it is an event the filter keeps.
func followLine(line []byte, filter *query.Filter, print func(map[string]any) error) error {
	var event map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(line))), &event); err != nil || event == nil {
		return nil
	}
	if !filter.Match(event) {
		return nil
	}
	return print(event)
}

// wait sleeps one poll interval and reports whether ctx is done.
func wait(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	case <-time.After(pollInterval):
		return false
	}
}
