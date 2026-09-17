package file

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"encoding/json"

	"github.com/jeremygprawira/wlog/drain/memory"
)

// maxLine is the longest line Read and Tail keep. A longer line is skipped and counted,
// because one bad line must never fail a read, and a cap keeps memory bounded (gate G4).
const maxLine = 1 << 20

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
	reader := bufio.NewReader(handle)

	lineNo := 0
	for {
		line, tooLong, err := readLine(reader)
		if errors.Is(err, io.EOF) {
			return events, skipped, nil
		}
		if err != nil {
			return events, skipped, err
		}
		lineNo++
		if tooLong {
			// A line past the cap is skipped and counted, and the read continues: the
			// spec is that one bad line never fails a whole read.
			skipped.Lines = append(skipped.Lines, lineNo)
			if skipped.Err == nil {
				skipped.Err = fmt.Errorf("line %d is longer than %d bytes", lineNo, maxLine)
			}
			continue
		}
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
}

// readLine returns one line without its trailing newline. A line past maxLine is
// reported as too long, and the rest of it is discarded, so the caller can read on. The
// returned error is io.EOF once the input ends.
func readLine(reader *bufio.Reader) (line string, tooLong bool, err error) {
	var buf []byte
	for {
		chunk, readErr := reader.ReadSlice('\n')
		if len(buf)+len(chunk) > maxLine {
			// Discard the rest of the oversized line without holding it in memory.
			for errors.Is(readErr, bufio.ErrBufferFull) {
				_, readErr = reader.ReadSlice('\n')
			}
			if readErr == nil || errors.Is(readErr, io.EOF) {
				return "", true, nil
			}
			return "", true, readErr
		}
		buf = append(buf, chunk...)
		switch {
		case readErr == nil:
			return strings.TrimSuffix(strings.TrimSuffix(string(buf), "\n"), "\r"), false, nil
		case errors.Is(readErr, bufio.ErrBufferFull):
			continue
		case errors.Is(readErr, io.EOF):
			if len(buf) == 0 {
				return "", false, io.EOF
			}
			return strings.TrimSuffix(string(buf), "\r"), false, nil
		default:
			return "", false, readErr
		}
	}
}

// parseLine decodes one NDJSON line, reporting false for a malformed one.
func parseLine(line string) (map[string]any, bool) {
	var event map[string]any
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		return nil, false
	}
	return event, true
}
