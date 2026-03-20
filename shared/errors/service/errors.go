package service

import (
	"errors"
	"fmt"
	"time"

	shared "github.com/nastyazhadan/spot-order-grpc/shared/errors"
)

var (
	ErrOrderNotFound      = shared.ErrNotFound{}
	ErrOrderAlreadyExists = shared.ErrAlreadyExists{}
	ErrMarketNotFound     = shared.ErrMarketNotFound{}

	ErrRateLimitExceeded = ErrLimitExceeded{}

	ErrMarketsNotFound      = errors.New("markets not found")
	ErrUserRoleNotSpecified = errors.New("user role not specified")

	ErrInvalidSubject    = errors.New("invalid subject in token")
	ErrInvalidJTI        = errors.New("invalid refresh token jti")
	ErrTokenRevoked      = errors.New("refresh token revoked or not found")
	ErrRevokeTokenFailed = errors.New("failed to revoke refresh token")
	ErrSaveTokenFailed   = errors.New("failed to save refresh token")
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
