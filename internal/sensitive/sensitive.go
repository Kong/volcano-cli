// Package sensitive marks errors whose text can carry file contents or secret
// values, such as a parser quoting the line it failed on. The text still
// reaches the terminal; usage telemetry replaces it with "[redacted]".
package sensitive

import "errors"

// Error wraps an error whose text must not leave the machine.
type Error struct {
	err error
}

// Wrap marks err as sensitive. It returns nil for a nil err.
func Wrap(err error) error {
	if err == nil {
		return nil
	}
	return &Error{err: err}
}

func (e *Error) Error() string { return e.err.Error() }

func (e *Error) Unwrap() error { return e.err }

// Text returns the text of the first sensitive error in err's chain, or ""
// when there is none.
func Text(err error) string {
	var sensitiveErr *Error
	if errors.As(err, &sensitiveErr) {
		return sensitiveErr.Error()
	}
	return ""
}
