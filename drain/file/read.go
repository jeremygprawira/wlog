package file

import (
	"bufio"
	"fmt"
	"os"

	"encoding/json"

	"github.com/jeremygprawira/wlog/drain/memory"
)

// ParseErrors reports the malformed lines Read skipped. A partial write or a hand edit
// leaves such a line, and one bad line must never fail a whole read.
type ParseErrors struct {
	Lines []int // 1-based line numbers, in order
	Err   error // the first parse error
}

// Error renders the skips, or "" when there were none.
func (p ParseErrors) Error() string {
	if len(p.Lines) == 0 {
		return ""
	}
	return fmt.Sprintf("file: skipped %d malformed line(s), first at line %d: %v", len(p.Lines), p.Lines[0], p.Err)
}

// Read walks the NDJSON file once and returns every line that passes the filter, oldest
// line first. It reuses memory.Filter, so one filter type covers the ring buffer and the
// file. A malformed line is skipped and counted in the returned ParseErrors.
func Read(path string, f memory.Filter) ([]map[string]any, ParseErrors, error) {
	handle, err := os.Open(path)
	if err != nil {
		return nil, ParseErrors{}, err
	}
	defer func() { _ = handle.Close() }()

	events := []map[string]any{}
	var skipped ParseErrors
	scanner := bufio.NewScanner(handle)
	scanner.Buffer(make([]byte, 64*1024), 1<<20)

	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		if line == "" {
			continue
		}
		event, ok := parseLine(line)
		if !ok {
			skipped.Lines = append(skipped.Lines, lineNo)
			if skipped.Err == nil {
				var decoded map[string]any
				skipped.Err = json.Unmarshal([]byte(line), &decoded)
			}
			continue
		}
		if memory.Matches(event, f) {
			events = append(events, event)
		}
	}
	if err := scanner.Err(); err != nil {
		return events, skipped, err
	}
	return events, skipped, nil
}

// parseLine decodes one NDJSON line, reporting false for a malformed one.
func parseLine(line string) (map[string]any, bool) {
	var event map[string]any
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		return nil, false
	}
	return event, true
}
