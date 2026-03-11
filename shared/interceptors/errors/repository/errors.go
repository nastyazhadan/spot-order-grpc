package repository

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
)

var (
	ErrOrderNotFound       = ErrNotFound{}
	ErrOrderAlreadyExists  = ErrAlreadyExists{}
	ErrMarketStoreIsEmpty  = errors.New("market store is empty")
	ErrMarketCacheNotFound = errors.New("market cache not found")
)

type ErrNotFound struct {
	ID uuid.UUID
}

func (e ErrNotFound) Error() string {
	return fmt.Sprintf("order with id=%s not found", e.ID)
}

func (e ErrNotFound) Is(target error) bool {
	var errorType ErrNotFound
	return errors.As(target, &errorType)
}

type ErrAlreadyExists struct {
	ID uuid.UUID
}

func (e ErrAlreadyExists) Error() string {
	return fmt.Sprintf("order with id=%s already exists", e.ID)
}

func (e ErrAlreadyExists) Is(target error) bool {
	var errorType ErrAlreadyExists
	return errors.As(target, &errorType)
}
