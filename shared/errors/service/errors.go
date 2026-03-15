package service

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

var (
	ErrOrderNotFound        = ErrNotFound{}
	ErrOrderAlreadyExists   = ErrAlreadyExists{}
	ErrRateLimitExceeded    = ErrLimitExceeded{}
	ErrMrktNotFound         = ErrMarketNotFound{}
	ErrMarketsNotFound      = errors.New("markets not found")
	ErrUserRoleNotSpecified = errors.New("user role not specified")
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

type ErrLimitExceeded struct {
	Limit  int64
	Window time.Duration
}

func (e ErrLimitExceeded) Error() string {
	return fmt.Sprintf("order rate limit exceeded: %d requests per %s", e.Limit, e.Window)
}

func (e ErrLimitExceeded) Is(target error) bool {
	var errorType ErrLimitExceeded
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
