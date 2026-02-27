//go:build integration

package tests

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/nastyazhadan/spot-order-grpc/orderService/tests/suite"
	proto "github.com/nastyazhadan/spot-order-grpc/shared/protos/gen/go/order/v1"
)

func TestGetOrderStatusHappyPath(test *testing.T) {
	ctx, st := suite.New(test)
	st.ClearOrders(ctx)

	market := suite.NewMarket()
	st.SetAvailableMarkets(market)

	userID := uuid.New().String()

	createResponse, err := st.OrderClient.CreateOrder(ctx, suite.ValidCreateRequest(userID, market.ID.String()))
	require.NoError(test, err)

	getResponse, err := st.OrderClient.GetOrderStatus(ctx, &proto.GetOrderStatusRequest{
		OrderId: createResponse.GetOrderId(),
		UserId:  userID,
	})
	require.NoError(test, err)
	require.NotNil(test, getResponse)

	assert.Equal(test, proto.OrderStatus_STATUS_CREATED, getResponse.GetStatus())
}

func TestCreatedStatusMatchesCreateResponse(test *testing.T) {
	ctx, st := suite.New(test)
	st.ClearOrders(ctx)

	market := suite.NewMarket()
	st.SetAvailableMarkets(market)
	userID := uuid.New().String()

	createResponse, err := st.OrderClient.CreateOrder(ctx, suite.ValidCreateRequest(userID, market.ID.String()))
	require.NoError(test, err)

	getResponse, err := st.OrderClient.GetOrderStatus(ctx, &proto.GetOrderStatusRequest{
		OrderId: createResponse.GetOrderId(),
		UserId:  userID,
	})
	require.NoError(test, err)

	assert.Equal(test, createResponse.GetStatus(), getResponse.GetStatus())
}

func TestMultipleOrdersReturnsCorrectStatus(test *testing.T) {
	ctx, st := suite.New(test)
	st.ClearOrders(ctx)

	market := suite.NewMarket()
	st.SetAvailableMarkets(market)

	user1 := uuid.New().String()
	user2 := uuid.New().String()

	createResponse1, err := st.OrderClient.CreateOrder(ctx, suite.ValidCreateRequest(user1, market.ID.String()))
	require.NoError(test, err)

	createResponse2, err := st.OrderClient.CreateOrder(ctx, suite.ValidCreateRequest(user2, market.ID.String()))
	require.NoError(test, err)

	getResponse1, err := st.OrderClient.GetOrderStatus(ctx, &proto.GetOrderStatusRequest{
		OrderId: createResponse1.GetOrderId(),
		UserId:  user1,
	})
	require.NoError(test, err)
	assert.Equal(test, proto.OrderStatus_STATUS_CREATED, getResponse1.GetStatus())

	getResponse2, err := st.OrderClient.GetOrderStatus(ctx, &proto.GetOrderStatusRequest{
		OrderId: createResponse2.GetOrderId(),
		UserId:  user2,
	})
	require.NoError(test, err)
	assert.Equal(test, proto.OrderStatus_STATUS_CREATED, getResponse2.GetStatus())
}

func TestGetOrderUUIDCornerCase(test *testing.T) {
	ctx, st := suite.New(test)

	market := suite.NewMarket()
	st.SetAvailableMarkets(market)

	tests := []struct {
		name   string
		userID string
	}{
		{
			name:   "uuid в верхнем регистре",
			userID: "550E8400-E29B-41D4-A716-446655440000",
		},
	}

	for _, tt := range tests {
		test.Run(tt.name, func(t *testing.T) {
			createResponse, err := st.OrderClient.CreateOrder(ctx, suite.ValidCreateRequest(tt.userID, market.ID.String()))
			require.NoError(t, err)

			getResponse, err := st.OrderClient.GetOrderStatus(ctx, &proto.GetOrderStatusRequest{
				OrderId: createResponse.GetOrderId(),
				UserId:  tt.userID,
			})

			require.NoError(t, err)
			assert.NotNil(t, getResponse)
			assert.Equal(t, proto.OrderStatus_STATUS_CREATED, getResponse.GetStatus())
		})
	}
}

func TestAnotherUserReturnsNotFound(test *testing.T) {
	ctx, st := suite.New(test)
	st.ClearOrders(ctx)

	market := suite.NewMarket()
	st.SetAvailableMarkets(market)

	ownerID := uuid.New().String()
	anotherUserID := uuid.New().String()

	createResponse, err := st.OrderClient.CreateOrder(ctx, suite.ValidCreateRequest(ownerID, market.ID.String()))
	require.NoError(test, err)

	getResponse, err := st.OrderClient.GetOrderStatus(ctx, &proto.GetOrderStatusRequest{
		OrderId: createResponse.GetOrderId(),
		UserId:  anotherUserID,
	})

	require.Error(test, err)
	assert.Nil(test, getResponse)

	stat, ok := status.FromError(err)
	require.True(test, ok)
	assert.Equal(test, codes.NotFound, stat.Code())
}

func TestOrderNotFound(test *testing.T) {
	ctx, st := suite.New(test)

	getResponse, err := st.OrderClient.GetOrderStatus(ctx, &proto.GetOrderStatusRequest{
		OrderId: uuid.New().String(),
		UserId:  uuid.New().String(),
	})

	require.Error(test, err)
	assert.Nil(test, getResponse)

	stat, _ := status.FromError(err)
	assert.Equal(test, codes.NotFound, stat.Code())
}

func TestNilUUIDReturnsNotFound(t *testing.T) {
	ctx, st := suite.New(t)

	getResponse, err := st.OrderClient.GetOrderStatus(ctx, &proto.GetOrderStatusRequest{
		OrderId: uuid.Nil.String(),
		UserId:  uuid.Nil.String(),
	})

	require.Error(t, err)
	assert.Nil(t, getResponse)

	stat, _ := status.FromError(err)
	assert.Equal(t, codes.NotFound, stat.Code())
}

func TestInvalidArguments(test *testing.T) {
	ctx, st := suite.New(test)

	validID := uuid.New().String()

	tests := []struct {
		name    string
		orderID string
		userID  string
	}{
		{
			name:    "пустой order_id",
			orderID: "",
			userID:  validID,
		},
		{
			name:    "пустой user_id",
			orderID: validID,
			userID:  "",
		},
		{
			name:    "оба пустые",
			orderID: "",
			userID:  "",
		},
		{
			name:    "невалидный order_id",
			orderID: "not-a-uuid",
			userID:  validID,
		},
		{
			name:    "невалидный user_id",
			orderID: validID,
			userID:  "not-a-uuid",
		},
		{
			name:    "числа вместо UUID",
			orderID: "12345",
			userID:  validID,
		},
		{
			name:    "UUID без дефисов",
			orderID: validID,
			userID:  "550e8400e29b41d4a716446655440000",
		},
	}

	for _, tt := range tests {
		test.Run(tt.name, func(t *testing.T) {
			getResponse, err := st.OrderClient.GetOrderStatus(ctx, &proto.GetOrderStatusRequest{
				OrderId: tt.orderID,
				UserId:  tt.userID,
			})

			require.Error(t, err)
			assert.Nil(t, getResponse)

			stat, ok := status.FromError(err)
			require.True(t, ok)
			assert.Equal(t, codes.InvalidArgument, stat.Code())
		})
	}
}

func TestDirectDBInsertOrderVisibleViaAPI(test *testing.T) {
	ctx, st := suite.New(test)
	st.ClearOrders(ctx)

	orderID := uuid.New()
	userID := uuid.New()
	marketID := uuid.New()

	_, err := st.Pool.Exec(ctx,
		`INSERT INTO orders (id, user_id, market_id, type, price, quantity, status, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		orderID, userID, marketID,
		1,
		"55.50",
		5,
		1,
		time.Now().UTC(),
	)
	require.NoError(test, err)

	getResponse, err := st.OrderClient.GetOrderStatus(ctx, &proto.GetOrderStatusRequest{
		OrderId: orderID.String(),
		UserId:  userID.String(),
	})
	require.NoError(test, err)
	assert.Equal(test, proto.OrderStatus_STATUS_CREATED, getResponse.GetStatus())
}

func TestAllStatusValuesReturnedCorrectly(test *testing.T) {
	ctx, st := suite.New(test)
	st.ClearOrders(ctx)

	statuses := []struct {
		dbStatus    int
		protoStatus proto.OrderStatus
	}{
		{1, proto.OrderStatus_STATUS_CREATED},
		{2, proto.OrderStatus_STATUS_PENDING},
		{3, proto.OrderStatus_STATUS_FILLED},
		{4, proto.OrderStatus_STATUS_CANCELLED},
	}

	userID := uuid.New()

	for _, stat := range statuses {
		s := stat
		test.Run(s.protoStatus.String(), func(t *testing.T) {
			orderID := uuid.New()
			_, err := st.Pool.Exec(ctx,
				`INSERT INTO orders (id, user_id, market_id, type, price, quantity, status, created_at)
				 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
				orderID, userID, uuid.New(),
				1, "100.00", 1, s.dbStatus, time.Now().UTC(),
			)
			require.NoError(t, err)

			getResponse, err := st.OrderClient.GetOrderStatus(ctx, &proto.GetOrderStatusRequest{
				OrderId: orderID.String(),
				UserId:  userID.String(),
			})
			require.NoError(t, err)
			assert.Equal(t, s.protoStatus, getResponse.GetStatus())
		})
	}
}

func TestFullFlowCreateThenGet(test *testing.T) {
	ctx, st := suite.New(test)
	st.ClearOrders(ctx)

	market := suite.NewMarket()
	st.SetAvailableMarkets(market)

	userID := uuid.New().String()

	createResponse, err := st.OrderClient.CreateOrder(ctx, suite.ValidCreateRequest(userID, market.ID.String()))
	require.NoError(test, err)
	require.Equal(test, proto.OrderStatus_STATUS_CREATED, createResponse.GetStatus())

	getResponse, err := st.OrderClient.GetOrderStatus(ctx, &proto.GetOrderStatusRequest{
		OrderId: createResponse.GetOrderId(),
		UserId:  userID,
	})
	require.NoError(test, err)
	assert.Equal(test, createResponse.GetStatus(), getResponse.GetStatus())

	assert.True(test, st.OrderExistsInDB(ctx, createResponse.GetOrderId()))
}
