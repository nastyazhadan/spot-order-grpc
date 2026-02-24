//go:build integration

package tests

import (
	"errors"
	"fmt"
	"math"
	"testing"

	"github.com/brianvoe/gofakeit/v6"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/type/decimal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/nastyazhadan/spot-order-grpc/orderService/tests/suite"
	proto "github.com/nastyazhadan/spot-order-grpc/shared/protos/gen/go/order/v6"
)

func TestCreateOrderHappyPath(test *testing.T) {
	ctx, st := suite.New(test)
	st.ClearOrders(ctx)

	market := suite.NewMarket()
	st.SetAvailableMarkets(market)

	userID := uuid.New().String()
	request := suite.ValidCreateRequest(userID, market.ID.String())

	response, err := st.OrderClient.CreateOrder(ctx, request)
	require.NoError(test, err)
	require.NotNil(test, response)

	assert.NotEmpty(test, response.GetOrderId())
	assert.Equal(test, proto.OrderStatus_STATUS_CREATED, response.GetStatus())

	assert.True(test, st.OrderExistsInDB(ctx, response.GetOrderId()))
	assert.Equal(test, 1, st.CountOrders(ctx))
}

func TestAllOrderTypesHappyPath(test *testing.T) {
	ctx, st := suite.New(test)
	market := suite.NewMarket()
	st.SetAvailableMarkets(market)

	orderTypes := []proto.OrderType{
		proto.OrderType_TYPE_LIMIT,
		proto.OrderType_TYPE_MARKET,
		proto.OrderType_TYPE_STOP_LOSS,
		proto.OrderType_TYPE_TAKE_PROFIT,
	}

	for _, orderType := range orderTypes {
		// orderType := orderType
		test.Run(orderType.String(), func(test *testing.T) {
			request := suite.ValidCreateRequest(uuid.New().String(), market.ID.String())
			request.OrderType = orderType

			response, err := st.OrderClient.CreateOrder(ctx, request)
			require.NoError(test, err)
			assert.Equal(test, proto.OrderStatus_STATUS_CREATED, response.GetStatus())
		})
	}
}

func TestDifferentUsersGetSeparateOrders(test *testing.T) {
	ctx, st := suite.New(test)
	st.ClearOrders(ctx)

	market := suite.NewMarket()
	st.SetAvailableMarkets(market)

	user1 := uuid.New().String()
	user2 := uuid.New().String()

	response1, err := st.OrderClient.CreateOrder(ctx, suite.ValidCreateRequest(user1, market.ID.String()))
	require.NoError(test, err)

	response2, err := st.OrderClient.CreateOrder(ctx, suite.ValidCreateRequest(user2, market.ID.String()))
	require.NoError(test, err)

	assert.NotEqual(test, response1.GetOrderId(), response2.GetOrderId())
	assert.Equal(test, 2, st.CountOrders(ctx))
}

func TestCreateOrderUUIDCornerCases(test *testing.T) {
	ctx, st := suite.New(test)

	market := suite.NewMarket()
	st.SetAvailableMarkets(market)

	tests := []struct {
		name         string
		userID       string
		marketID     string
		shouldPass   bool
		expectedCode codes.Code
	}{
		{
			name:         "uuid без дефисов",
			userID:       "550e8400e29b41d4a716446655440000",
			marketID:     market.ID.String(),
			shouldPass:   false,
			expectedCode: codes.InvalidArgument,
		},
		{
			name:       "uuid в верхнем регистре",
			userID:     "550E8400-E29B-41D4-A716-446655440000",
			marketID:   market.ID.String(),
			shouldPass: true,
		},
	}

	for _, tt := range tests {
		test.Run(tt.name, func(t *testing.T) {
			request := suite.ValidCreateRequest(tt.userID, tt.marketID)

			response, err := st.OrderClient.CreateOrder(ctx, request)
			if tt.shouldPass {
				require.NoError(t, err)
				require.NotNil(t, response)
				assert.NotEmpty(t, response.GetOrderId())
				return
			}

			assert.Nil(t, response)
			stat, ok := status.FromError(err)
			require.True(t, ok)
			assert.Equal(t, tt.expectedCode, stat.Code())
		})
	}
}

func TestPriceCornerCases(test *testing.T) {
	ctx, st := suite.New(test)
	market := suite.NewMarket()
	st.SetAvailableMarkets(market)

	userID := uuid.New().String()
	marketID := market.ID.String()

	tests := []struct {
		name         string
		price        string
		expectedCode codes.Code
		shouldPass   bool
	}{
		{
			name:       "минимально допустимая цена 0.00000001",
			price:      "0.00000001",
			shouldPass: true,
		},
		{
			name:       "очень большая цена",
			price:      "9999999999.99999999",
			shouldPass: true,
		},
		{
			name:         "цена 0",
			price:        "0",
			expectedCode: codes.InvalidArgument,
		},
		{
			name:         "отрицательная цена",
			price:        "-1.5",
			expectedCode: codes.InvalidArgument,
		},
		{
			name:         "пустая цена",
			price:        "",
			expectedCode: codes.InvalidArgument,
		},
		{
			name:         "цена не число",
			price:        "abc",
			expectedCode: codes.InvalidArgument,
		},
		{
			name:         "цена NaN",
			price:        "NaN",
			expectedCode: codes.InvalidArgument,
		},
		{
			name:         "цена Infinity",
			price:        "Inf",
			expectedCode: codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		test.Run(tt.name, func(t *testing.T) {
			request := suite.ValidCreateRequest(userID, marketID)
			request.Price = &decimal.Decimal{Value: tt.price}

			response, err := st.OrderClient.CreateOrder(ctx, request)
			if tt.shouldPass {
				require.NoError(t, err)
				assert.NotEmpty(t, response.GetOrderId())
			} else {
				require.Error(t, err)
				assert.Nil(t, response)
				stat, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tt.expectedCode, stat.Code())
			}
		})
	}
}

func TestQuantityCornerCases(test *testing.T) {
	ctx, st := suite.New(test)
	market := suite.NewMarket()
	st.SetAvailableMarkets(market)

	userID := uuid.New().String()
	marketID := market.ID.String()

	tests := []struct {
		name         string
		quantity     int64
		expectedCode codes.Code
		shouldPass   bool
	}{
		{
			name:       "минимальное количество = 1",
			quantity:   1,
			shouldPass: true,
		},
		{
			name:       "большое количество",
			quantity:   1_000_000,
			shouldPass: true,
		},
		{
			name:       "максимальный int64",
			quantity:   math.MaxInt64,
			shouldPass: true,
		},
		{
			name:         "количество = 0",
			quantity:     0,
			expectedCode: codes.InvalidArgument,
		},
		{
			name:         "отрицательное количество",
			quantity:     -1,
			expectedCode: codes.InvalidArgument,
		},
		{
			name:         "минимальный отрицательный int64",
			quantity:     math.MinInt64,
			expectedCode: codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		test.Run(tt.name, func(t *testing.T) {
			request := suite.ValidCreateRequest(userID, marketID)
			request.Quantity = tt.quantity

			response, err := st.OrderClient.CreateOrder(ctx, request)
			if tt.shouldPass {
				require.NoError(t, err)
				assert.NotEmpty(t, response.GetOrderId())
			} else {
				require.Error(t, err)
				assert.Nil(t, response)
				stat, _ := status.FromError(err)
				assert.Equal(t, tt.expectedCode, stat.Code())
			}
		})
	}
}

func TestInvalidUUIDs(test *testing.T) {
	ctx, st := suite.New(test)

	validID := uuid.New().String()
	market := suite.NewMarket()
	st.SetAvailableMarkets(market)

	tests := []struct {
		name     string
		userID   string
		marketID string
	}{
		{
			name:     "пустой user_id",
			userID:   "",
			marketID: validID,
		},
		{
			name:     "пустой market_id",
			userID:   validID,
			marketID: "",
		},
		{
			name:     "оба пустые",
			userID:   "",
			marketID: "",
		},
		{
			name:     "невалидный user_id",
			userID:   "not-a-uuid",
			marketID: validID,
		},
		{
			name:     "невалидный market_id",
			userID:   validID,
			marketID: "not-a-uuid",
		},
		{
			name:     "числа вместо UUID",
			userID:   "12345",
			marketID: validID,
		},
	}

	for _, tt := range tests {
		test.Run(tt.name, func(t *testing.T) {
			request := suite.ValidCreateRequest(tt.userID, tt.marketID)
			response, err := st.OrderClient.CreateOrder(ctx, request)

			require.Error(t, err)
			assert.Nil(t, response)

			stat, ok := status.FromError(err)
			require.True(t, ok)
			assert.Equal(t, codes.InvalidArgument, stat.Code())
		})
	}
}

func TestMarketNotFoundReturnsNotFound(test *testing.T) {
	ctx, st := suite.New(test)

	st.SetAvailableMarkets()

	request := suite.ValidCreateRequest(uuid.New().String(), uuid.New().String())
	response, err := st.OrderClient.CreateOrder(ctx, request)

	require.Error(test, err)
	assert.Nil(test, response)

	stat, _ := status.FromError(err)
	assert.Equal(test, codes.NotFound, stat.Code())
	assert.Equal(test, 0, st.CountOrders(ctx))
}

func TestMarketExistsButWrongIDReturnsNotFound(test *testing.T) {
	ctx, st := suite.New(test)

	st.SetAvailableMarkets(suite.NewMarket())

	request := suite.ValidCreateRequest(uuid.New().String(), uuid.New().String())
	response, err := st.OrderClient.CreateOrder(ctx, request)

	require.Error(test, err)
	assert.Nil(test, response)

	stat, _ := status.FromError(err)
	assert.Equal(test, codes.NotFound, stat.Code())
}

func TestSpotServiceUnavailableReturnsInternalError(test *testing.T) {
	ctx, st := suite.New(test)

	st.SetMarketViewerError(errors.New("spot service connection refused"))

	request := suite.ValidCreateRequest(uuid.New().String(), uuid.New().String())
	response, err := st.OrderClient.CreateOrder(ctx, request)

	require.Error(test, err)
	assert.Nil(test, response)

	stat, _ := status.FromError(err)
	assert.Equal(test, codes.Internal, stat.Code())
	assert.Equal(test, 0, st.CountOrders(ctx))
}

func TestOrderTypeUnspecifiedReturnsInvalidArgument(test *testing.T) {
	ctx, st := suite.New(test)
	market := suite.NewMarket()
	st.SetAvailableMarkets(market)

	request := suite.ValidCreateRequest(uuid.New().String(), market.ID.String())
	request.OrderType = proto.OrderType_TYPE_UNSPECIFIED

	response, err := st.OrderClient.CreateOrder(ctx, request)
	require.Error(test, err)
	assert.Nil(test, response)

	stat, _ := status.FromError(err)
	assert.Equal(test, codes.InvalidArgument, stat.Code())
}

func TestNilPriceReturnsInvalidArgument(test *testing.T) {
	ctx, st := suite.New(test)
	market := suite.NewMarket()
	st.SetAvailableMarkets(market)

	request := suite.ValidCreateRequest(uuid.New().String(), market.ID.String())
	request.Price = nil

	response, err := st.OrderClient.CreateOrder(ctx, request)
	require.Error(test, err)
	assert.Nil(test, response)

	stat, _ := status.FromError(err)
	assert.Equal(test, codes.InvalidArgument, stat.Code())
}

func TestTwoOrdersSameUserBothSucceed(test *testing.T) {
	ctx, st := suite.New(test)
	st.ClearOrders(ctx)

	market := suite.NewMarket()
	st.SetAvailableMarkets(market)

	userID := uuid.New().String()
	request := suite.ValidCreateRequest(userID, market.ID.String())

	response1, err := st.OrderClient.CreateOrder(ctx, request)
	require.NoError(test, err)

	response2, err := st.OrderClient.CreateOrder(ctx, request)
	require.NoError(test, err)

	assert.NotEqual(test, response1.GetOrderId(), response2.GetOrderId())
	assert.Equal(test, 2, st.CountOrders(ctx))
}

func TestConcurrentRequests(test *testing.T) {
	ctx, st := suite.New(test)
	st.ClearOrders(ctx)

	market := suite.NewMarket()
	st.SetAvailableMarkets(market)

	const goroutines = 20
	results := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			request := suite.ValidCreateRequest(uuid.New().String(), market.ID.String())
			request.Price = &decimal.Decimal{
				Value: fmt.Sprintf("%.2f", gofakeit.Float64Range(1, 9999)),
			}
			request.Quantity = int64(gofakeit.IntRange(1, 1000))
			_, err := st.OrderClient.CreateOrder(ctx, request)
			results <- err
		}()
	}

	for i := 0; i < goroutines; i++ {
		err := <-results
		assert.NoError(test, err)
	}

	assert.Equal(test, goroutines, st.CountOrders(ctx))
}
