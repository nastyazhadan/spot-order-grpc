package consumer

import (
	"fmt"
)

type RetryExhaustedError struct {
	Err        error
	RetryCount int
}

func (e RetryExhaustedError) Error() string {
	return e.Err.Error()
}

func (e RetryExhaustedError) Unwrap() error {
	return e.Err
}

type MessageTooLargeError struct {
	Size  int
	Limit int
}

func (e MessageTooLargeError) Error() string {
	return fmt.Sprintf("kafka message too large: %d > %d", e.Size, e.Limit)
}
