package audit

import (
	"reflect"
	"sort"
	"strings"

	"github.com/jeremygprawira/wlog/redact"
)

// Operation is one RFC 6902 JSON Patch operation: what to do, to which path, and with
// which value. A remove carries no value.
type Operation struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value any    `json:"value,omitempty"`
}

// PatchOption configures Patch.
type PatchOption func(*patchConfig)

// patchConfig holds the resolved settings of one Patch call.
type patchConfig struct {
	redactor *redact.Redactor
}

// WithRedactor sets the redactor Patch consults before it writes a value. The default is
// redact.Default(), so a patch built without an option still hides the keys the default
// denylist names. Pass the Logger's own redactor when the service uses a custom one.
func WithRedactor(r *redact.Redactor) PatchOption {
	return func(c *patchConfig) { c.redactor = r }
}

// Patch returns the RFC 6902 operations that turn before into after, in a stable order.
//
// Read top to bottom: the two values must be objects, and each changed leaf becomes one
// operation: add for a key only after holds, remove for a key only before holds, and
// replace for a key both hold with different values. Nested objects walk down into their
// own paths. A key holding a slash or a tilde is escaped the JSON Pointer way, so the path
// always names exactly one key.
//
// A path the redactor denies carries the replacement text instead of the value, so a patch
// can describe a change to a secret without carrying the secret. The event's own redactor
// then runs again on the way out, which is the second line of defence.
//
// It returns an error when either side is not an object, because a slice or a scalar has
// no keys to compare.
func Patch(before, after any, opts ...PatchOption) ([]Operation, error) {
	left, err := toTree(before)
	if err != nil {
		return nil, err
	}
	right, err := toTree(after)
	if err != nil {
		return nil, err
	}
	cfg := patchConfig{redactor: redact.Default()}
	for _, opt := range opts {
		opt(&cfg)
	}

	var ops []Operation
	patchObjects(left, right, "", cfg, &ops)
	return ops, nil
}

// patchObjects walks two objects in sorted key order and appends one operation per changed
// leaf below prefix.
func patchObjects(before, after map[string]any, prefix string, cfg patchConfig, ops *[]Operation) {
	keys := make([]string, 0, len(before)+len(after))
	for key := range unionKeys(before, after) {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		path := prefix + "/" + escapePointer(key)
		left, leftOK := before[key]
		right, rightOK := after[key]

		leftMap, leftIsMap := left.(map[string]any)
		rightMap, rightIsMap := right.(map[string]any)
		if leftIsMap && rightIsMap {
			patchObjects(leftMap, rightMap, path, cfg, ops)
			continue
		}

		switch {
		case !leftOK:
			*ops = append(*ops, Operation{Op: "add", Path: path, Value: cfg.value(path, right)})
		case !rightOK:
			*ops = append(*ops, Operation{Op: "remove", Path: path})
		case !reflect.DeepEqual(left, right):
			*ops = append(*ops, Operation{Op: "replace", Path: path, Value: cfg.value(path, right)})
		}
	}
}

// value returns the value an operation carries, or the replacement text when the denylist
// denies the operation's path.
func (c patchConfig) value(pointer string, value any) any {
	if c.redactor == nil {
		return value
	}
	if c.redactor.DeniesPath(pathSegments(pointer)...) {
		return c.redactor.Replacement()
	}
	return value
}

// pathSegments returns the keys a JSON Pointer names, unescaped.
func pathSegments(pointer string) []string {
	segments := strings.Split(strings.TrimPrefix(pointer, "/"), "/")
	for i, segment := range segments {
		segments[i] = unescapePointer(segment)
	}
	return segments
}

// escapePointer escapes one JSON Pointer segment: "~" becomes "~0" and "/" becomes "~1".
func escapePointer(segment string) string {
	return strings.ReplaceAll(strings.ReplaceAll(segment, "~", "~0"), "/", "~1")
}

// unescapePointer reverses escapePointer, so a path can be matched against a denylist.
func unescapePointer(segment string) string {
	return strings.ReplaceAll(strings.ReplaceAll(segment, "~1", "/"), "~0", "~")
}
