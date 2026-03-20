package repository

import (
	"errors"

	shared "github.com/nastyazhadan/spot-order-grpc/shared/errors"
)

var (
	ErrOrderNotFound      = shared.ErrNotFound{}
	ErrOrderAlreadyExists = shared.ErrAlreadyExists{}
	ErrMarketNotFound     = shared.ErrMarketNotFound{}

	ErrMarketStoreIsEmpty = errors.New("market store is empty")
	ErrMarketsNotFound    = errors.New("markets cache not found")
)
