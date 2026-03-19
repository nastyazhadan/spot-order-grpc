package service

import (
	"errors"
	"fmt"

	"github.com/google/uuid"

	shared "github.com/nastyazhadan/spot-order-grpc/shared/errors"
)

var (
	ErrOrderNotFound      = shared.ErrNotFound{}
	ErrOrderAlreadyExists = shared.ErrAlreadyExists{}
	ErrRateLimitExceeded  = shared.ErrLimitExceeded{}

	ErrMrktNotFound         = ErrMarketNotFound{}
	ErrMarketsNotFound      = errors.New("markets not found")
	ErrUserRoleNotSpecified = errors.New("user role not specified")
)

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
