package repository

import "errors"

var (
	ErrOrderNotFound      = errors.New("order not found")
	ErrOrderAlreadyExists = errors.New("order already exists")
	ErrMarketStoreIsEmpty = errors.New("market store is empty")
)
