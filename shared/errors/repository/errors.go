package repository

import (
	"errors"

	shared "github.com/nastyazhadan/spot-order-grpc/shared/errors"
)

var (
	ErrOrderNotFound      = shared.ErrNotFound{}
	ErrOrderAlreadyExists = shared.ErrAlreadyExists{}

	ErrCacheNotFound       = errors.New("cache not found")
	ErrMarketStoreIsEmpty  = errors.New("market store is empty")
	ErrMarketCacheNotFound = errors.New("market cache not found")
)
