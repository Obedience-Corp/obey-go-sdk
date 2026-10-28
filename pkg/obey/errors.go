package obey

import "fmt"

func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Operation, e.Code, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Operation, e.Code)
}
func (e *Error) Unwrap() error { return e.Cause }
