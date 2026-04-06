package consumer

import (
	"errors"
	"fmt"
)

var (
	ErrMessageHandledByDLQ    = errors.New("message handled by dlq")
	ErrRestartConsumerSession = errors.New("restart consumer session")
	ErrSkipMessage            = errors.New("skip message")
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

func IsControlFlowError(err error) bool {
	return errors.Is(err, ErrSkipMessage) ||
		errors.Is(err, ErrRestartConsumerSession) ||
		errors.Is(err, ErrMessageHandledByDLQ)
}

func IsNonRetryableError(err error) bool {
	var tooLargeError MessageTooLargeError

	return errors.As(err, &tooLargeError) || IsControlFlowError(err)
}
