package utils

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// TimeoutError is returned when an operation exceeded its timeout
type TimeoutError struct {
	Operation string
	Timeout   time.Duration
	Err       error
}

func (e *TimeoutError) Error() string {
	return fmt.Sprintf("%s timed out after %s: %v", e.Operation, e.Timeout, e.Err)
}

func (e *TimeoutError) Unwrap() error {
	return e.Err
}

// ContextWithOptionalTimeout returns a context that expires after timeout, or
// a context without deadline if timeout is 0
func ContextWithOptionalTimeout(timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout > 0 {
		return context.WithTimeout(context.Background(), timeout)
	}
	return context.WithCancel(context.Background())
}

// WrapTimeout wraps err in a TimeoutError if ctx exceeded its deadline
func WrapTimeout(ctx context.Context, operation string, timeout time.Duration, err error) error {
	if err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return &TimeoutError{Operation: operation, Timeout: timeout, Err: err}
	}
	return err
}
