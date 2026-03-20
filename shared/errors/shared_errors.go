package errors

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
)

var (
	ErrCacheNotFound = errors.New("cache not found")

	MsgRequestRequired = "request is required"
)

type ErrNotFound struct {
	ID uuid.UUID
}

func (e ErrNotFound) Error() string {
	return fmt.Sprintf("resource with id=%s not found", e.ID)
}

func (e ErrNotFound) Is(target error) bool {
	var errorType ErrNotFound
	return errors.As(target, &errorType)
}

type ErrAlreadyExists struct {
	ID uuid.UUID
}

func (e ErrAlreadyExists) Error() string {
	return fmt.Sprintf("resource with id=%s already exists", e.ID)
}

func (e ErrAlreadyExists) Is(target error) bool {
	var errorType ErrAlreadyExists
	return errors.As(target, &errorType)
}

type ErrMarketNotFound struct {
	ID uuid.UUID
}

func (e ErrMarketNotFound) Error() string {
	return fmt.Sprintf("market with id=%s not found", e.ID)
}

func (e ErrMarketNotFound) Is(target error) bool {
	var errorType ErrMarketNotFound
	return errors.As(target, &errorType)
}
