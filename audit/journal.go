package audit

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/jeremygprawira/wlog"
)

// markerEvery is how many records land between two marker lines. A marker states the
// count and the head of the chain, so a reader can see whether lines went missing.
const markerEvery = 100

// chainPlaceholder is 64 zeros, written where the chain hash belongs while Journal
// builds the line. The hash covers those zeros, so replacing them with the real hash
// later does not change the covered bytes.
const chainPlaceholder = "0000000000000000000000000000000000000000000000000000000000000000"

// Journal returns a wlog.Drain that appends one NDJSON line per audit-flagged event to
// path, fsync'd after every write (durability over throughput, since audit logs are low
// volume). Non-audit events pass through untouched.
//
// Read top to bottom: Send takes one lock, chains the event against the previous line,
// writes the line, and syncs it. Nothing else touches the file, so concurrent events
// land in chain order. The hash covers the exact bytes of the line it writes, so a
// single changed byte anywhere in a line fails Verify.
//
// On the first use it reads the file's last line to recover the previous hash, so a
// restart continues the same chain. WithKey signs every line, and Verify then rejects
// an edit that does not carry a matching signature.
//
// The file mode is 0600. The chain fields are added to the event, so a drain listed
// after Journal sees them too.
func Journal(path string, opts ...Option) wlog.Drain {
	j := &journal{path: path}
	for _, o := range opts {
		o(j)
	}
	return j
}

// Option configures a Journal.
type Option func(*journal)

// journal owns one journal file and its chain state.
type journal struct {
	// mu serializes hashing, writing, and the chain state, so the bytes on disk are
	// always in chain order.
	mu   sync.Mutex
	path string
	file *os.File
	prev string
	key  []byte
	// records counts the record lines this file holds, markers excluded.
	records int
}

// Send chains one audit event and appends it.
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

	if err := j.append(event); err != nil {
		return
	}
	j.records++
	if j.records%markerEvery == 0 {
		_ = j.appendMarker()
	}
}

// Close writes a final marker and closes the file, so a reader learns how many records
// the file holds. A journal that never received a record writes nothing.
func (j *journal) Close(context.Context) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.file == nil {
		return nil
	}
	if j.records > 0 {
		_ = j.appendMarker()
	}
	err := j.file.Close()
	j.file = nil
	return err
}

// append chains one line for event and writes it. The caller must hold j.mu.
func (j *journal) append(event map[string]any) error {
	event["audit.prev_hash"] = j.prev
	line, hash, err := j.seal(event)
	if err != nil {
		return err
	}
	if _, err := j.file.Write(line); err != nil {
		return err
	}
	if err := j.file.Sync(); err != nil {
		return err
	}
	j.prev = hash
	return nil
}

// appendMarker writes the line that says how many records the chain covers and the hash
// they end at. With a key the marker also carries the key id, so a reader knows which
// key to ask for.
func (j *journal) appendMarker() error {
	body := map[string]any{"count": j.records, "head": j.prev}
	if j.key != nil {
		body["key_id"] = keyID(j.key)
	}
	return j.append(map[string]any{"audit.marker": body})
}

// seal returns the exact line bytes for event, plus the chain hash it wrote.
//
// The hash covers the whole line with the value of audit.hash, and of audit.signature
// when a key is set, replaced by zeros. Every other byte therefore belongs to the
// chain, so JSON whitespace, a duplicate key, or a changed number fails Verify. The
// caller must hold j.mu.
func (j *journal) seal(event map[string]any) (line []byte, hash string, err error) {
	event["audit.hash"] = chainPlaceholder
	if j.key != nil {
		event["audit.signature"] = chainPlaceholder
	}
	line, err = json.Marshal(event)
	if err != nil {
		return nil, "", err
	}
	_, hashOff, err := chainValue(line, "audit.hash")
	if err != nil {
		return nil, "", err
	}
	offsets := []int{hashOff}
	sigOff := -1
	if j.key != nil {
		_, sigOff, err = chainValue(line, "audit.signature")
		if err != nil {
			return nil, "", err
		}
		offsets = append(offsets, sigOff)
	}
	sum := sha256.Sum256(coveredBytes(line, offsets...))
	hash = hex.EncodeToString(sum[:])
	copy(line[hashOff:hashOff+64], hash)
	event["audit.hash"] = hash
	if sigOff >= 0 {
		sig := signature(j.key, hash)
		copy(line[sigOff:sigOff+64], sig)
		event["audit.signature"] = sig
	}
	return append(line, '\n'), hash, nil
}

// chainValue returns the 64-character hex value of a chain field in one JSON line, and
// its offset. It reports a missing, duplicated, or malformed field, so a line that
// carries two audit.hash keys can never be treated as valid.
func chainValue(line []byte, field string) (value []byte, offset int, err error) {
	marker := []byte(`"` + field + `":"`)
	first := bytes.Index(line, marker)
	if first < 0 {
		return nil, 0, fmt.Errorf("missing %s", field)
	}
	if bytes.Contains(line[first+1:], marker) {
		return nil, 0, fmt.Errorf("duplicate %s", field)
	}
	start := first + len(marker)
	if start+64 >= len(line) || line[start+64] != '"' {
		return nil, 0, fmt.Errorf("malformed %s", field)
	}
	value = line[start : start+64]
	if _, err := hex.DecodeString(string(value)); err != nil {
		return nil, 0, fmt.Errorf("malformed %s", field)
	}
	return value, start, nil
}

// coveredBytes returns line with every 64-byte value at one of offsets replaced by
// zeros, which is the byte sequence the chain hash covers.
func coveredBytes(line []byte, offsets ...int) []byte {
	cp := append([]byte(nil), line...)
	for _, off := range offsets {
		for i := 0; i < 64; i++ {
			cp[off+i] = '0'
		}
	}
	return cp
}

// ensureOpen opens the file once and resumes the chain from its last line.
func (j *journal) ensureOpen() error {
	if j.file != nil {
		return nil
	}
	f, err := os.OpenFile(j.path, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	j.file = f
	records, last, ok := scanFile(f)
	j.records = records
	if ok {
		if h, _, err := chainValue(last, "audit.hash"); err == nil {
			j.prev = string(h)
		}
	}
	return nil
}

// scanFile returns how many record lines f holds, and its last non-empty line, read
// from the start. Writes after this still land at end-of-file: f was opened with
// os.O_APPEND, which repositions the offset before every write regardless of where
// reads left it.
func scanFile(f *os.File) (records int, last []byte, found bool) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return 0, nil, false
	}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1<<20)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		last = append(last[:0], line...)
		found = true
		if isMarker(line) {
			continue
		}
		records++
	}
	return records, last, found
}

// isMarker reports whether one line holds a marker rather than a record. It parses the
// line, so a record that merely mentions the word is still counted as a record.
func isMarker(line []byte) bool {
	if !bytes.Contains(line, []byte(`"audit.marker"`)) {
		return false
	}
	var rec map[string]any
	if err := json.Unmarshal(line, &rec); err != nil {
		return false
	}
	_, ok := rec["audit.marker"]
	return ok
}
