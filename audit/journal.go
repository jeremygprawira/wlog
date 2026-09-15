package audit

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"sync"

	"github.com/jeremygprawira/wlog"
)

// Journal returns a wlog.Drain that appends one NDJSON line per audit-flagged event to
// path, fsync'd after every write (durability over throughput, since audit logs are low
// volume). Like Chain, it hash-links every event it writes (audit.hash =
// sha256(prev_hash || canonical_json(event))), computed after redaction since Journal
// runs as an ordinary drain in the pipeline. On first use it reads the file's last
// existing line to resume that chain, so a process restart continues it instead of
// silently starting a new one. Non-audit events pass through untouched.
func Journal(path string) wlog.Drain {
	return &journal{path: path}
}

type journal struct {
	mu   sync.Mutex
	path string
	file *os.File
	prev string
}

func (j *journal) Send(_ context.Context, event map[string]any) {
	if _, ok := event["audit"]; !ok {
		return
	}

	j.mu.Lock()
	defer j.mu.Unlock()

	// ponytail: a journal is best-effort per gate G3 (never block or panic the
	// caller) — any failure below just drops this line rather than propagating.
	if err := j.ensureOpen(); err != nil {
		return
	}

	canon, err := canonicalJSON(event)
	if err != nil {
		return
	}
	sum := sha256.Sum256(append([]byte(j.prev), canon...))
	hash := hex.EncodeToString(sum[:])
	event["audit.prev_hash"] = j.prev
	event["audit.hash"] = hash

	line, err := json.Marshal(event)
	if err != nil {
		return
	}
	line = append(line, '\n')
	if _, err := j.file.Write(line); err != nil {
		return
	}
	if err := j.file.Sync(); err != nil {
		return
	}
	j.prev = hash
}

func (j *journal) ensureOpen() error {
	if j.file != nil {
		return nil
	}
	f, err := os.OpenFile(j.path, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	j.file = f
	if last, ok := lastLine(f); ok {
		var rec map[string]any
		if err := json.Unmarshal([]byte(last), &rec); err == nil {
			if h, ok := rec["audit.hash"].(string); ok {
				j.prev = h
			}
		}
	}
	return nil
}

// lastLine returns the final non-empty line of f, read from the start. Writes after
// this still land at end-of-file: f was opened with os.O_APPEND, which repositions the
// offset before every write regardless of where reads left it.
func lastLine(f *os.File) (string, bool) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", false
	}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1<<20)
	var last string
	found := false
	for scanner.Scan() {
		if line := scanner.Text(); line != "" {
			last = line
			found = true
		}
	}
	return last, found
}
