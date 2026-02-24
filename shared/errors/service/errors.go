package service

import "errors"

var (
	ErrMarketsNotFound          = errors.New("markets not found")
	ErrOrderNotFound            = errors.New("order not found")
	ErrOrderAlreadyExists       = errors.New("order already exists")
	ErrUserRoleUnknown          = errors.New("user role unknown")
	ErrCreatingOrderNotRequired = errors.New("creating order not required")
	ErrRateLimitExceeded        = errors.New("order rate limit exceeded")
)
