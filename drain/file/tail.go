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
// each new one. A rotation, meaning the path shrinks or is replaced, reopens the file
// from the start. The channel closes once ctx is done.
func Tail(ctx context.Context, path string, f memory.Filter) (<-chan map[string]any, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	out := make(chan map[string]any)
	go func() {
		defer close(out)
		handle, err := os.Open(path)
		if err != nil {
			return
		}
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

			if info, err := os.Stat(path); err == nil && info.Size() < offset {
				_ = handle.Close()
				handle, err = os.Open(path)
				if err != nil {
					return
				}
				offset = 0
				pending = ""
			}

			if _, err := handle.Seek(offset, io.SeekStart); err != nil {
				continue
			}
			buf := make([]byte, 32*1024)
			for {
				n, readErr := handle.Read(buf)
				if n > 0 {
					offset += int64(n)
					pending += string(buf[:n])
					for {
						index := strings.IndexByte(pending, '\n')
						if index < 0 {
							break
						}
						line := pending[:index]
						pending = pending[index+1:]
						if event, ok := parseLine(line); ok && memory.Matches(event, f) {
							select {
							case out <- event:
							case <-ctx.Done():
								return
							}
						}
					}
				}
				if readErr != nil {
					break
				}
			}
		}
	}()
	return out, nil
}
