//go:build integration

package tests

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/brianvoe/gofakeit/v6"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/type/decimal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/nastyazhadan/spot-order-grpc/orderService/tests/suite"
	proto "github.com/nastyazhadan/spot-order-grpc/protos/gen/go/common/v1"
)

func TestHappyPath(test *testing.T) {
	ctx, st := suite.New(test)
	st.ClearOrders(ctx)

	market := suite.NewMarket()
	st.SetAvailableMarkets(market)

	userID := uuid.New()
	authCtx := st.CtxWithUserID(ctx, userID)

	response, err := st.OrderClient.CreateOrder(authCtx, suite.ValidCreateRequest(market.ID.String()))
	require.NoError(test, err)
	require.NotNil(test, response)

	assert.NotEmpty(test, response.GetOrderId())
	assert.Equal(test, proto.OrderStatus_STATUS_CREATED, response.GetStatus())
	assert.True(test, st.OrderExistsInDB(ctx, response.GetOrderId()))
	assert.Equal(test, 1, st.CountOrders(ctx))
}

func TestAllOrderTypes(test *testing.T) {
	ctx, st := suite.New(test)
	market := suite.NewMarket()
	st.SetAvailableMarkets(market)

	st.ClearOrders(ctx)
	for _, orderType := range []proto.OrderType{
		proto.OrderType_TYPE_LIMIT,
		proto.OrderType_TYPE_MARKET,
		proto.OrderType_TYPE_STOP_LOSS,
		proto.OrderType_TYPE_TAKE_PROFIT,
	} {
		test.Run(orderType.String(), func(t *testing.T) {
			authCtx := st.CtxWithUserID(ctx, uuid.New())
			request := suite.ValidCreateRequest(market.ID.String())
			request.OrderType = orderType

			response, err := st.OrderClient.CreateOrder(authCtx, request)
			require.NoError(t, err)
			assert.Equal(t, proto.OrderStatus_STATUS_CREATED, response.GetStatus())
		})
	}
}

func TestDifferentUsersGetSeparateOrders(test *testing.T) {
	ctx, st := suite.New(test)
	st.ClearOrders(ctx)

	market := suite.NewMarket()
	st.SetAvailableMarkets(market)

	firstResponse, err := st.OrderClient.CreateOrder(
		st.CtxWithUserID(ctx, uuid.New()),
		suite.ValidCreateRequest(market.ID.String()),
	)
	require.NoError(test, err)

	secondResponse, err := st.OrderClient.CreateOrder(
		st.CtxWithUserID(ctx, uuid.New()),
		suite.ValidCreateRequest(market.ID.String()),
	)
	require.NoError(test, err)

	assert.NotEqual(test, firstResponse.GetOrderId(), secondResponse.GetOrderId())
	assert.Equal(test, 2, st.CountOrders(ctx))
}

func TestSameUserTwoDifferentOrders(test *testing.T) {
	ctx, st := suite.New(test)
	st.ClearOrders(ctx)

	market := suite.NewMarket()
	st.SetAvailableMarkets(market)

	userID := uuid.New()
	authCtx := st.CtxWithUserID(ctx, userID)

	firstRequest := suite.ValidCreateRequest(market.ID.String())
	firstRequest.Price = &decimal.Decimal{Value: "100.00"}

	secondRequest := suite.ValidCreateRequest(market.ID.String())
	secondRequest.Price = &decimal.Decimal{Value: "200.00"}

	firstResponse, err := st.OrderClient.CreateOrder(authCtx, firstRequest)
	require.NoError(test, err)

	secondResponse, err := st.OrderClient.CreateOrder(authCtx, secondRequest)
	require.NoError(test, err)

	assert.NotEqual(test, firstResponse.GetOrderId(), secondResponse.GetOrderId())
	assert.Equal(test, 2, st.CountOrders(ctx))
}

func TestConcurrentRequests(test *testing.T) {
	ctx, st := suite.New(test)
	st.ClearOrders(ctx)

	market := suite.NewMarket()
	st.SetAvailableMarkets(market)

	const goroutines = 20
	var wg sync.WaitGroup
	errs := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			authCtx := st.CtxWithUserID(ctx, uuid.New())
			request := suite.ValidCreateRequest(market.ID.String())
			request.Price = &decimal.Decimal{
				Value: fmt.Sprintf("%.2f", gofakeit.Float64Range(1, 9999)),
			}
			request.Quantity = int64(gofakeit.IntRange(1, 1000))
			_, err := st.OrderClient.CreateOrder(authCtx, request)
			errs <- err
		}()
	}

	go func() {
		wg.Wait()
		close(errs)
	}()

	for err := range errs {
		assert.NoError(test, err)
	}
	assert.Equal(test, goroutines, st.CountOrders(ctx))
}

func TestNoUserID(test *testing.T) {
	ctx, st := suite.New(test)
	market := suite.NewMarket()
	st.SetAvailableMarkets(market)

	response, err := st.OrderClient.CreateOrder(ctx, suite.ValidCreateRequest(market.ID.String()))
	require.Error(test, err)
	assert.Nil(test, response)

	st2, ok := status.FromError(err)
	require.True(test, ok)
	assert.Equal(test, codes.Unauthenticated, st2.Code())
}

func TestInvalidMarketID(test *testing.T) {
	ctx, st := suite.New(test)

	tests := []struct {
		name     string
		marketID string
	}{
		{"пустой market_id", ""},
		{"невалидный UUID", "not-a-uuid"},
		{"числа", "12345"},
		{"UUID без дефисов", "550e8400e29b41d4a716446655440000"},
	}

	for _, tt := range tests {
		test.Run(tt.name, func(t *testing.T) {
			authCtx := st.CtxWithUserID(ctx, uuid.New())
			response, err := st.OrderClient.CreateOrder(authCtx, suite.ValidCreateRequest(tt.marketID))

			require.Error(t, err)
			assert.Nil(t, response)
			st2, ok := status.FromError(err)
			require.True(t, ok)
			assert.Equal(t, codes.InvalidArgument, st2.Code())
		})
	}
}

func TestMarketIDUpperCase(test *testing.T) {
	ctx, st := suite.New(test)
	market := suite.NewMarket()
	st.SetAvailableMarkets(market)

	authCtx := st.CtxWithUserID(ctx, uuid.New())
	request := suite.ValidCreateRequest(market.ID.String())

	request.MarketId = "550E8400-E29B-41D4-A716-446655440000"

	response, err := st.OrderClient.CreateOrder(authCtx, request)
	if err == nil {
		assert.NotEmpty(test, response.GetOrderId())
	} else {
		st2, ok := status.FromError(err)
		require.True(test, ok)
		assert.Equal(test, codes.NotFound, st2.Code())
	}
}

func TestPriceValidation(test *testing.T) {
	ctx, st := suite.New(test)
	market := suite.NewMarket()
	st.SetAvailableMarkets(market)
	authCtx := st.CtxWithUserID(ctx, uuid.New())

	tests := []struct {
		name       string
		price      string
		wantCode   codes.Code
		shouldPass bool
	}{
		{"минимально допустимая 0.00000001", "0.00000001", 0, true},
		{"максимально допустимая 9999999999.99999999", "9999999999.99999999", 0, true},
		{"целое число", "500", 0, true},
		{"цена=0", "0", codes.InvalidArgument, false},
		{"отрицательная", "-1.5", codes.InvalidArgument, false},
		{"пустая строка", "", codes.InvalidArgument, false},
		{"не число", "abc", codes.InvalidArgument, false},
		{"NaN", "NaN", codes.InvalidArgument, false},
		{"Infinity", "Inf", codes.InvalidArgument, false},
		{"слишком много дробных знаков", "1.123456789", codes.InvalidArgument, false},
		{"слишком много целых знаков", "12345678901.0", codes.InvalidArgument, false},
	}

	for _, tt := range tests {
		test.Run(tt.name, func(t *testing.T) {
			request := suite.ValidCreateRequest(market.ID.String())
			request.Price = &decimal.Decimal{Value: tt.price}

			response, err := st.OrderClient.CreateOrder(authCtx, request)
			if tt.shouldPass {
				require.NoError(t, err)
				assert.NotEmpty(t, response.GetOrderId())
			} else {
				require.Error(t, err)
				assert.Nil(t, response)
				st2, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tt.wantCode, st2.Code())
			}
		})
	}
}

func TestNilPrice(test *testing.T) {
	ctx, st := suite.New(test)
	market := suite.NewMarket()
	st.SetAvailableMarkets(market)

	request := suite.ValidCreateRequest(market.ID.String())
	request.Price = nil

	response, err := st.OrderClient.CreateOrder(st.CtxWithUserID(ctx, uuid.New()), request)
	require.Error(test, err)
	assert.Nil(test, response)

	st2, _ := status.FromError(err)
	assert.Equal(test, codes.InvalidArgument, st2.Code())
}

func TestQuantityValidation(test *testing.T) {
	ctx, st := suite.New(test)
	market := suite.NewMarket()
	st.SetAvailableMarkets(market)
	authCtx := st.CtxWithUserID(ctx, uuid.New())

	tests := []struct {
		name       string
		quantity   int64
		wantCode   codes.Code
		shouldPass bool
	}{
		{"минимальное 1", 1, 0, true},
		{"большое 1_000_000", 1_000_000, 0, true},
		{"максимальный int64", math.MaxInt64, 0, true},
		{"quantity=0", 0, codes.InvalidArgument, false},
		{"отрицательное -1", -1, codes.InvalidArgument, false},
		{"минимальный int64", math.MinInt64, codes.InvalidArgument, false},
	}

	for _, tt := range tests {
		test.Run(tt.name, func(t *testing.T) {
			request := suite.ValidCreateRequest(market.ID.String())
			request.Quantity = tt.quantity

			response, err := st.OrderClient.CreateOrder(authCtx, request)
			if tt.shouldPass {
				require.NoError(t, err)
				assert.NotEmpty(t, response.GetOrderId())
			} else {
				require.Error(t, err)
				assert.Nil(t, response)
				st2, _ := status.FromError(err)
				assert.Equal(t, tt.wantCode, st2.Code())
			}
		})
	}
}

func TestOrderTypeUnspecified(test *testing.T) {
	ctx, st := suite.New(test)
	market := suite.NewMarket()
	st.SetAvailableMarkets(market)

	request := suite.ValidCreateRequest(market.ID.String())
	request.OrderType = proto.OrderType_TYPE_UNSPECIFIED

	response, err := st.OrderClient.CreateOrder(st.CtxWithUserID(ctx, uuid.New()), request)
	require.Error(test, err)
	assert.Nil(test, response)

	st2, _ := status.FromError(err)
	assert.Equal(test, codes.InvalidArgument, st2.Code())
}

func TestMarketNotFound(test *testing.T) {
	ctx, st := suite.New(test)
	st.SetAvailableMarkets()

	authCtx := st.CtxWithUserID(ctx, uuid.New())
	response, err := st.OrderClient.CreateOrder(authCtx, suite.ValidCreateRequest(uuid.New().String()))

	require.Error(test, err)
	assert.Nil(test, response)

	st2, _ := status.FromError(err)
	assert.Equal(test, codes.NotFound, st2.Code())
	assert.Equal(test, 0, st.CountOrders(ctx))
}

func TestMarketExistsButWrongID(test *testing.T) {
	ctx, st := suite.New(test)
	st.SetAvailableMarkets(suite.NewMarket())

	authCtx := st.CtxWithUserID(ctx, uuid.New())
	response, err := st.OrderClient.CreateOrder(authCtx, suite.ValidCreateRequest(uuid.New().String()))

	require.Error(test, err)
	assert.Nil(test, response)

	st2, _ := status.FromError(err)
	assert.Equal(test, codes.NotFound, st2.Code())
}

func TestMarketDisabled(test *testing.T) {
	ctx, st := suite.New(test)

	disabledMarket := suite.NewMarket()
	disabledMarket.Enabled = false
	st.SetAvailableMarkets(disabledMarket)

	authCtx := st.CtxWithUserID(ctx, uuid.New())
	response, err := st.OrderClient.CreateOrder(authCtx, suite.ValidCreateRequest(disabledMarket.ID.String()))

	require.Error(test, err)
	assert.Nil(test, response)

	st2, _ := status.FromError(err)
	assert.Equal(test, codes.FailedPrecondition, st2.Code())
	assert.Equal(test, 0, st.CountOrders(ctx))
}

func TestMarketDeleted(test *testing.T) {
	ctx, st := suite.New(test)

	deletedMarket := suite.NewMarket()
	now := time.Now().UTC()
	deletedMarket.DeletedAt = &now
	st.SetAvailableMarkets(deletedMarket)

	authCtx := st.CtxWithUserID(ctx, uuid.New())
	response, err := st.OrderClient.CreateOrder(authCtx, suite.ValidCreateRequest(deletedMarket.ID.String()))

	require.Error(test, err)
	assert.Nil(test, response)

	st2, _ := status.FromError(err)
	assert.Equal(test, codes.NotFound, st2.Code())
	assert.Equal(test, 0, st.CountOrders(ctx))
}

func TestSpotServiceUnavailable(test *testing.T) {
	ctx, st := suite.New(test)
	st.SetMarketViewerError(errors.New("spot service connection refused"))

	authCtx := st.CtxWithUserID(ctx, uuid.New())
	response, err := st.OrderClient.CreateOrder(authCtx, suite.ValidCreateRequest(uuid.New().String()))

	require.Error(test, err)
	assert.Nil(test, response)

	st2, _ := status.FromError(err)
	assert.Equal(test, codes.Internal, st2.Code())
	assert.Equal(test, 0, st.CountOrders(ctx))
}

func TestDuplicateRequestReturnsExistingOrder(test *testing.T) {
	ctx, st := suite.New(test)
	st.ClearOrders(ctx)

	market := suite.NewMarket()
	st.SetAvailableMarkets(market)

	userID := uuid.New()
	authCtx := st.CtxWithUserID(ctx, userID)
	request := suite.ValidCreateRequest(market.ID.String())

	firstResponse, err := st.OrderClient.CreateOrder(authCtx, request)
	require.NoError(test, err)

	secondResponse, err := st.OrderClient.CreateOrder(authCtx, request)
	require.NoError(test, err)

	assert.Equal(test, firstResponse.GetOrderId(), secondResponse.GetOrderId())
	assert.Equal(test, 1, st.CountOrders(ctx), "дубликат не должен создавать новую запись")
}

func TestDifferentPricesDifferentOrders(test *testing.T) {
	ctx, st := suite.New(test)
	st.ClearOrders(ctx)

	market := suite.NewMarket()
	st.SetAvailableMarkets(market)

	userID := uuid.New()
	authCtx := st.CtxWithUserID(ctx, userID)

	firstRequest := suite.ValidCreateRequest(market.ID.String())
	firstRequest.Price = &decimal.Decimal{Value: "100.00"}

	secondRequest := suite.ValidCreateRequest(market.ID.String())
	secondRequest.Price = &decimal.Decimal{Value: "101.00"}

	firstResponse, err := st.OrderClient.CreateOrder(authCtx, firstRequest)
	require.NoError(test, err)

	secondResponse, err := st.OrderClient.CreateOrder(authCtx, secondRequest)
	require.NoError(test, err)

	assert.NotEqual(test, firstResponse.GetOrderId(), secondResponse.GetOrderId(), "разные параметры → разные ордера")
	assert.Equal(test, 2, st.CountOrders(ctx))
}
