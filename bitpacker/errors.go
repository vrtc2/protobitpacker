package bitpacker

import "fmt"

// ValidationError is returned by Validate() when a field is missing a required annotation
// or has an invalid one.
type ValidationError struct {
	Message string // fully-qualified message name
	Field   string // field name (empty for oneof-level errors)
	Reason  string
}

func (e *ValidationError) Error() string {
	if e.Field == "" {
		return fmt.Sprintf("bitpacker: message %s: %s", e.Message, e.Reason)
	}
	return fmt.Sprintf("bitpacker: message %s field %s: %s", e.Message, e.Field, e.Reason)
}

// PackError is returned by Pack() when a value is out of range for its declared bit width.
type PackError struct {
	Field  string
	Reason string
}

func (e *PackError) Error() string {
	return fmt.Sprintf("bitpacker: pack field %s: %s", e.Field, e.Reason)
}

// UnpackError is returned by Unpack() when the bit stream is malformed.
type UnpackError struct {
	Field  string
	Reason string
}

func (e *UnpackError) Error() string {
	return fmt.Sprintf("bitpacker: unpack field %s: %s", e.Field, e.Reason)
}

// ErrUnexpectedEOF is returned when the bit stream ends prematurely.
var ErrUnexpectedEOF = fmt.Errorf("bitpacker: unexpected end of bit stream")
