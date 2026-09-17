package file

import (
	"context"
	"io"
	"os"
	"strings"
	"time"

	"github.com/jeremygprawira/wlog/drain/memory"
)

// tailPollInterval is how often Tail checks the file. Polling needs no third-party
// watcher, which keeps this package in the root module.
const tailPollInterval = 200 * time.Millisecond

// Tail follows a file the way tail -f does: it yields the existing matching lines, then
// each new one. It recognizes a rotation by comparing the file identity with os.SameFile,
// so a new file that already outgrew the old offset is still noticed. Before it switches,
// it reads what the old file still held, so no line is lost. The channel closes once ctx
// is done.
func Tail(ctx context.Context, path string, f memory.Filter) (<-chan map[string]any, error) {
	handle, info, err := openTail(path)
	if err != nil {
		return nil, err
	}
	out := make(chan map[string]any)
	go func() {
		defer close(out)
		defer func() { _ = handle.Close() }()

		var offset int64
		var pending string
		ticker := time.NewTicker(tailPollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}

			if current, err := os.Stat(path); err == nil {
				switch {
				case !os.SameFile(info, current):
					// A rotation replaced the file. Read what the old one still holds
					// before switching, so its last lines are not lost.
					offset = drainReader(handle, offset, &pending, out, f, ctx)
					_ = handle.Close()
					handle, info, err = openTail(path)
					if err != nil {
						return
					}
					offset, pending = 0, ""
				case current.Size() < offset:
					// The same file, cut short: read it again from the start.
					offset, pending = 0, ""
				}
			}

			offset = drainReader(handle, offset, &pending, out, f, ctx)
		}
	}()
	return out, nil
}

// openTail opens path and reports the identity of the file it opened.
func openTail(path string) (*os.File, os.FileInfo, error) {
	handle, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	info, err := handle.Stat()
	if err != nil {
		_ = handle.Close()
		return nil, nil, err
	}
	return handle, info, nil
}

// drainReader reads every complete line the handle holds now, sends the matching ones,
// and returns the new offset. A line longer than maxLine is discarded, so a file without a
// newline cannot grow the pending text without bound (gate G4).
func drainReader(handle *os.File, offset int64, pending *string, out chan<- map[string]any, f memory.Filter, ctx context.Context) int64 {
	if _, err := handle.Seek(offset, io.SeekStart); err != nil {
		return offset
	}
	buf := make([]byte, 32*1024)
	for {
		n, readErr := handle.Read(buf)
		if n > 0 {
			offset += int64(n)
			*pending += string(buf[:n])
			for {
				index := strings.IndexByte(*pending, '\n')
				if index < 0 {
					break
				}
				line := (*pending)[:index]
				*pending = (*pending)[index+1:]
				if event, ok := parseLine(line); ok && memory.Matches(event, f) {
					select {
					case out <- event:
					case <-ctx.Done():
						return offset
					}
				}
			}
			if len(*pending) > maxLine {
				// No newline arrived within the cap, so the rest is not a line this
				// reader will ever use.
				*pending = ""
			}
		}
		if readErr != nil {
			return offset
		}
	}
}
