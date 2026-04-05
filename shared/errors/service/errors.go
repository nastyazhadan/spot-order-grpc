package service

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	shared "github.com/nastyazhadan/spot-order-grpc/shared/errors"
)

var (
	ErrOrderNotFound      = shared.ErrNotFound{}
	ErrOrderAlreadyExists = shared.ErrAlreadyExists{}
	ErrMarketNotFound     = shared.ErrMarketNotFound{}

	ErrRateLimitExceeded = ErrLimitExceeded{}
	ErrMarketUnavailable = ErrUnavailable{}
	ErrMarketDisabled    = ErrDisabled{}

	ErrMarketsNotFound      = errors.New("markets not found")
	ErrMarketsUnavailable   = errors.New("markets are temporarily unavailable")
	ErrUserRoleNotSpecified = errors.New("user role not specified")

	ErrInvalidSubject    = errors.New("invalid subject in token")
	ErrInvalidJTI        = errors.New("invalid refresh token jti")
	ErrTokenRevoked      = errors.New("refresh token revoked or not found")
	ErrRevokeTokenFailed = errors.New("failed to revoke refresh token")
	ErrSaveTokenFailed   = errors.New("failed to save refresh token")

	ErrNilContext              = errors.New("outbox worker: nil context")
	ErrInvalidPagination       = errors.New("invalid pagination parameters")
	ErrSessionValidationFailed = errors.New("failed to validate session")
)

type ErrLimitExceeded struct {
	Limit  int64
	Window time.Duration
}

func (e ErrLimitExceeded) Error() string {
	return fmt.Sprintf("rate limit exceeded: %d requests per %s", e.Limit, e.Window)
}

func (e ErrLimitExceeded) Is(target error) bool {
	var errorType ErrLimitExceeded
	return errors.As(target, &errorType)
}

type ErrUnavailable struct {
	ID uuid.UUID
}

func (e ErrUnavailable) Error() string {
	return fmt.Sprintf("market with id=%s is temporarily unavailable", e.ID)
}

func (e ErrUnavailable) Is(target error) bool {
	var errorType ErrUnavailable
	return errors.As(target, &errorType)
}

type ErrDisabled struct {
	ID uuid.UUID
}

func (e ErrDisabled) Error() string {
	return fmt.Sprintf("market with id=%s is disabled", e.ID)
}

func (e ErrDisabled) Is(target error) bool {
	var errorType ErrDisabled
	return errors.As(target, &errorType)
}
