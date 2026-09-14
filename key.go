package wlog

import "context"

// Key is a compile-time-checked field name: Key[string]'s Set only accepts a string,
// so a typo in the value's type is a build error instead of a bad log line found
// later. Declare one per field you log often:
//
//	var OrderID = wlog.NewKey[string]("order_id")
//	OrderID.Set(ctx, order.ID)  // order.ID must be a string
//	OrderID.Set(ctx, 42)        // compile error: 42 (untyped int) is not a string
//
// Set(ctx, key, value) still works for anything not worth declaring a Key for.
type Key[T any] struct{ name string }

// NewKey declares a typed key named name.
func NewKey[T any](name string) Key[T] {
	return Key[T]{name: name}
}

// Set stores v under this key's name on the current event, exactly like
// Set(ctx, k.Name(), v) but restricted at compile time to values of type T.
func (k Key[T]) Set(ctx context.Context, v T) {
	Set(ctx, k.name, v)
}

// Name returns the key's field name.
func (k Key[T]) Name() string { return k.name }

// StrictKeys turns on unknown-key detection in local/dev environments (per
// WithService's env argument): an untyped Set call using a name not among keys is
// recorded in wlog.unknown_keys on that event, to catch a typo'd field name during
// development. Outside local/dev this is a no-op — it never rejects or drops the
// write, in any environment.
func StrictKeys(keys ...interface{ Name() string }) Option {
	return func(l *Logger) {
		if l.strictKeys == nil {
			l.strictKeys = map[string]bool{}
		}
		for _, k := range keys {
			l.strictKeys[k.Name()] = true
		}
	}
}

// strictKeysForEvent returns the registered key set to check new events against, or
// nil if strict mode is off or the current environment isn't local/dev.
func (l *Logger) strictKeysForEvent() map[string]bool {
	if l.strictKeys == nil {
		return nil
	}
	if l.service.env != "local" && l.service.env != "dev" {
		return nil
	}
	return l.strictKeys
}
