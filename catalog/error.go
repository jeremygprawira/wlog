package catalog

// CodedError is the error Registry.Err returns. errors.As reaches it, and its methods
// expose the full code, the entry, and the wrapped cause.
type CodedError interface {
	error
	Code() string // the full, prefixed code
	Entry() Entry // the entry the code resolved to
	Cause() error // the wrapped cause, or nil
}

// codedError implements CodedError.
type codedError struct {
	prefix  string
	full    string
	message string
	entry   Entry
	cause   error
}

// Error returns the rendered message.
func (e *codedError) Error() string { return e.message }

// Unwrap returns the cause, so errors.Is and errors.As reach it.
func (e *codedError) Unwrap() error { return e.cause }

// Cause returns the wrapped cause.
func (e *codedError) Cause() error { return e.cause }

// Code returns the full, prefixed code.
func (e *codedError) Code() string { return e.full }

// Entry returns the entry the code resolved to.
func (e *codedError) Entry() Entry { return e.entry }

// Is makes errors.Is(err, entry) true for this entry, and true for another coded error
// with the same full code.
func (e *codedError) Is(target error) bool {
	switch t := target.(type) {
	case *codedError:
		return t.full == e.full
	case Entry:
		return fullCode(e.prefix, t.Code) == e.full
	default:
		return false
	}
}
