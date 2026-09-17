package audit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	// unlock releases the exclusive lock this journal holds on the file.
	unlock func()
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
	if j.unlock != nil {
		j.unlock()
		j.unlock = nil
	}
	j.file = nil
	j.syncDir()
	return err
}

// syncDir syncs the directory that holds the journal, so the file itself survives a
// crash, not only its contents. Not every platform or filesystem allows it, and a
// failure there is not worth a problem report: the write already synced the bytes.
func (j *journal) syncDir() {
	d, err := os.Open(filepath.Dir(j.path))
	if err != nil {
		return
	}
	defer func() { _ = d.Close() }()
	_ = d.Sync()
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

// ensureOpen locks the file once and resumes the chain it already holds.
func (j *journal) ensureOpen() error {
	if j.file != nil {
		return nil
	}
	f, err := os.OpenFile(j.path, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	unlock, err := lockFile(f)
	if err != nil {
		_ = f.Close()
		return err
	}
	if err := j.resume(f); err != nil {
		unlock()
		_ = f.Close()
		return err
	}
	j.file, j.unlock = f, unlock
	return nil
}

// resume repairs a partial last line and recovers the chain head and the record count
// this file already holds, so a process that died mid-write continues the chain rather
// than starting a second one.
//
// The whole file is read into memory: audit journals are low volume, and a fixed read
// buffer would either cut a large record or refuse it.
func (j *journal) resume(f *os.File) error {
	data, err := io.ReadAll(f)
	if err != nil {
		return err
	}
	body, tail := splitTail(data)
	records, prev, err := replay(body, j.key)
	if err != nil {
		return err
	}
	if len(tail) > 0 {
		if hash, _, err := checkLine(tail, prev, j.key); err == nil {
			// Every byte of the line arrived and only the newline was lost, so keep
			// the record instead of moving a valid line aside.
			if _, err := f.Write(append(tail, '\n')); err != nil {
				return err
			}
			records++
			prev = hash
		} else {
			// The process died mid-write. Keep the half line next to the journal, so a
			// reader can still see what it was writing, and cut it off the chain.
			if err := appendPartial(j.path+".partial", tail); err != nil {
				return err
			}
			if err := f.Truncate(int64(len(body))); err != nil {
				return err
			}
		}
	}
	j.records, j.prev = records, prev
	return nil
}

// splitTail separates the terminated lines of data from an unterminated tail. Journal
// always ends a line with a newline, so bytes after the last one mean a crash mid-write
// or a lost newline.
func splitTail(data []byte) (body, tail []byte) {
	i := bytes.LastIndexByte(data, '\n')
	if i < 0 {
		return nil, data
	}
	return data[:i+1], data[i+1:]
}

// replay walks the terminated lines of a journal and returns how many records it holds
// and the head of its chain. A line that does not match the chain stops it: a journal
// with a tampered line is refused rather than continued.
func replay(body []byte, key []byte) (records int, prev string, err error) {
	for _, line := range bytes.Split(body, []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		hash, marker, err := checkLine(line, prev, key)
		if err != nil {
			return 0, "", fmt.Errorf("audit: existing journal: %w", err)
		}
		if marker == nil {
			records++
		}
		prev = hash
	}
	return records, prev, nil
}

// appendPartial keeps an unterminated tail next to the journal.
func appendPartial(path string, tail []byte) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Write(tail); err != nil {
		return err
	}
	return f.Sync()
}
