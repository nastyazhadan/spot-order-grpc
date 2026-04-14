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
	protoCommon "github.com/nastyazhadan/spot-order-grpc/protos/gen/go/common/v1"
	proto "github.com/nastyazhadan/spot-order-grpc/protos/gen/go/order/v1"
)

func TestGetOrderStatusHappyPath(test *testing.T) {
	ctx, st := suite.New(test)
	st.ClearOrders(ctx)

	market := suite.NewMarket()
	st.SetAvailableMarkets(market)

	userID := uuid.New()
	authCtx := st.CtxWithUserID(ctx, userID)

	createResponse, err := st.OrderClient.CreateOrder(authCtx, suite.ValidCreateRequest(market.ID.String()))
	require.NoError(test, err)

	getResponse, err := st.OrderClient.GetOrderStatus(authCtx, &proto.GetOrderStatusRequest{
		OrderId: createResponse.GetOrderId(),
	})
	require.NoError(test, err)
	require.NotNil(test, getResponse)

	assert.Equal(test, protoCommon.OrderStatus_STATUS_CREATED, getResponse.GetStatus())
}

func TestStatusMatchesCreateResponse(test *testing.T) {
	ctx, st := suite.New(test)
	st.ClearOrders(ctx)

	market := suite.NewMarket()
	st.SetAvailableMarkets(market)

	userID := uuid.New()
	authCtx := st.CtxWithUserID(ctx, userID)

	createResponse, err := st.OrderClient.CreateOrder(authCtx, suite.ValidCreateRequest(market.ID.String()))
	require.NoError(test, err)

	getResponse, err := st.OrderClient.GetOrderStatus(authCtx, &proto.GetOrderStatusRequest{
		OrderId: createResponse.GetOrderId(),
	})
	require.NoError(test, err)

	assert.Equal(test, createResponse.GetStatus(), getResponse.GetStatus())
}

func TestMultipleOrders(test *testing.T) {
	ctx, st := suite.New(test)
	st.ClearOrders(ctx)

	market := suite.NewMarket()
	st.SetAvailableMarkets(market)

	user1, user2 := uuid.New(), uuid.New()

	firstCreateResponse, err := st.OrderClient.CreateOrder(
		st.CtxWithUserID(ctx, user1),
		suite.ValidCreateRequest(market.ID.String()),
	)
	require.NoError(test, err)

	secondCreateResponse, err := st.OrderClient.CreateOrder(
		st.CtxWithUserID(ctx, user2),
		suite.ValidCreateRequest(market.ID.String()),
	)
	require.NoError(test, err)

	firstGetResponse, err := st.OrderClient.GetOrderStatus(
		st.CtxWithUserID(ctx, user1),
		&proto.GetOrderStatusRequest{OrderId: firstCreateResponse.GetOrderId()},
	)
	require.NoError(test, err)
	assert.Equal(test, protoCommon.OrderStatus_STATUS_CREATED, firstGetResponse.GetStatus())

	secondGetResponse, err := st.OrderClient.GetOrderStatus(
		st.CtxWithUserID(ctx, user2),
		&proto.GetOrderStatusRequest{OrderId: secondCreateResponse.GetOrderId()},
	)
	require.NoError(test, err)
	assert.Equal(test, protoCommon.OrderStatus_STATUS_CREATED, secondGetResponse.GetStatus())
}

func TestFullFlowCreateThenGet(test *testing.T) {
	ctx, st := suite.New(test)
	st.ClearOrders(ctx)

	market := suite.NewMarket()
	st.SetAvailableMarkets(market)

	userID := uuid.New()
	authCtx := st.CtxWithUserID(ctx, userID)

	createResponse, err := st.OrderClient.CreateOrder(authCtx, suite.ValidCreateRequest(market.ID.String()))
	require.NoError(test, err)
	require.Equal(test, protoCommon.OrderStatus_STATUS_CREATED, createResponse.GetStatus())

	getResponse, err := st.OrderClient.GetOrderStatus(authCtx, &proto.GetOrderStatusRequest{
		OrderId: createResponse.GetOrderId(),
	})
	require.NoError(test, err)
	assert.Equal(test, createResponse.GetStatus(), getResponse.GetStatus())
	assert.True(test, st.OrderExistsInDB(ctx, createResponse.GetOrderId()))
}

func TestAllStatusValues(test *testing.T) {
	ctx, st := suite.New(test)
	st.ClearOrders(ctx)

	statuses := []struct {
		dbStatus    int
		protoStatus protoCommon.OrderStatus
	}{
		{1, protoCommon.OrderStatus_STATUS_CREATED},
		{2, protoCommon.OrderStatus_STATUS_PENDING},
		{3, protoCommon.OrderStatus_STATUS_FILLED},
		{4, protoCommon.OrderStatus_STATUS_CANCELLED},
	}

	userID := uuid.New()
	authCtx := st.CtxWithUserID(ctx, userID)

	for _, stat := range statuses {
		test.Run(stat.protoStatus.String(), func(t *testing.T) {
			orderID := uuid.New()
			st.InsertOrderDirectly(ctx, orderID, userID, uuid.New(), stat.dbStatus)

			getResponse, err := st.OrderClient.GetOrderStatus(authCtx, &proto.GetOrderStatusRequest{
				OrderId: orderID.String(),
			})
			require.NoError(t, err)
			assert.Equal(t, stat.protoStatus, getResponse.GetStatus())
		})
	}
}

func TestNoUserIDUnauthenticated(test *testing.T) {
	ctx, st := suite.New(test)

	getResponse, err := st.OrderClient.GetOrderStatus(ctx, &proto.GetOrderStatusRequest{
		OrderId: uuid.New().String(),
	})

	require.Error(test, err)
	assert.Nil(test, getResponse)

	st2, ok := status.FromError(err)
	require.True(test, ok)
	assert.Equal(test, codes.Unauthenticated, st2.Code())
}

func TestAnotherUserOrderNotFound(test *testing.T) {
	ctx, st := suite.New(test)
	st.ClearOrders(ctx)

	market := suite.NewMarket()
	st.SetAvailableMarkets(market)

	ownerID := uuid.New()

	createResponse, err := st.OrderClient.CreateOrder(
		st.CtxWithUserID(ctx, ownerID),
		suite.ValidCreateRequest(market.ID.String()),
	)
	require.NoError(test, err)

	anotherCtx := st.CtxWithUserID(ctx, uuid.New())
	getResponse, err := st.OrderClient.GetOrderStatus(anotherCtx, &proto.GetOrderStatusRequest{
		OrderId: createResponse.GetOrderId(),
	})

	require.Error(test, err)
	assert.Nil(test, getResponse)

	st2, ok := status.FromError(err)
	require.True(test, ok)
	assert.Equal(test, codes.NotFound, st2.Code())
}

func TestOrderNotFound(test *testing.T) {
	ctx, st := suite.New(test)

	getResponse, err := st.OrderClient.GetOrderStatus(
		st.CtxWithUserID(ctx, uuid.New()),
		&proto.GetOrderStatusRequest{OrderId: uuid.New().String()},
	)

	require.Error(test, err)
	assert.Nil(test, getResponse)

	st2, _ := status.FromError(err)
	assert.Equal(test, codes.NotFound, st2.Code())
}

func TestNilUUID(test *testing.T) {
	ctx, st := suite.New(test)

	getResponse, err := st.OrderClient.GetOrderStatus(
		st.CtxWithUserID(ctx, uuid.New()),
		&proto.GetOrderStatusRequest{OrderId: uuid.Nil.String()},
	)

	require.Error(test, err)
	assert.Nil(test, getResponse)

	st2, _ := status.FromError(err)
	assert.Equal(test, codes.NotFound, st2.Code())
}

func TestInvalidOrderID(test *testing.T) {
	ctx, st := suite.New(test)

	tests := []struct {
		name    string
		orderID string
	}{
		{"пустой order_id", ""},
		{"невалидный UUID", "not-a-uuid"},
		{"числа", "12345"},
		{"UUID без дефисов", "550e8400e29b41d4a716446655440000"},
	}

	for _, tt := range tests {
		test.Run(tt.name, func(t *testing.T) {
			getResponse, err := st.OrderClient.GetOrderStatus(
				st.CtxWithUserID(ctx, uuid.New()),
				&proto.GetOrderStatusRequest{OrderId: tt.orderID},
			)

			require.Error(t, err)
			assert.Nil(t, getResponse)

			st2, ok := status.FromError(err)
			require.True(t, ok)
			assert.Equal(t, codes.InvalidArgument, st2.Code())
		})
	}
}

func TestNilRequest(test *testing.T) {
	ctx, st := suite.New(test)

	getResponse, err := st.OrderClient.GetOrderStatus(
		st.CtxWithUserID(ctx, uuid.New()),
		nil,
	)

	require.Error(test, err)
	assert.Nil(test, getResponse)

	st2, _ := status.FromError(err)
	assert.Equal(test, codes.InvalidArgument, st2.Code())
}

func TestUUIDUpperCase(test *testing.T) {
	ctx, st := suite.New(test)
	st.ClearOrders(ctx)

	market := suite.NewMarket()
	st.SetAvailableMarkets(market)

	userID := uuid.New()

	createResponse, err := st.OrderClient.CreateOrder(
		st.CtxWithUserID(ctx, userID),
		suite.ValidCreateRequest(market.ID.String()),
	)
	require.NoError(test, err)

	getResponse, err := st.OrderClient.GetOrderStatus(
		st.CtxWithUserID(ctx, userID),
		&proto.GetOrderStatusRequest{OrderId: createResponse.GetOrderId()},
	)
	require.NoError(test, err)
	assert.Equal(test, protoCommon.OrderStatus_STATUS_CREATED, getResponse.GetStatus())
}

func TestSameOrderIDDifferentUser(test *testing.T) {
	ctx, st := suite.New(test)
	st.ClearOrders(ctx)

	market := suite.NewMarket()
	st.SetAvailableMarkets(market)

	ownerID := uuid.New()
	anotherID := uuid.New()

	createResponse, err := st.OrderClient.CreateOrder(
		st.CtxWithUserID(ctx, ownerID),
		suite.ValidCreateRequest(market.ID.String()),
	)
	require.NoError(test, err)

	ownerResponse, err := st.OrderClient.GetOrderStatus(
		st.CtxWithUserID(ctx, ownerID),
		&proto.GetOrderStatusRequest{OrderId: createResponse.GetOrderId()},
	)
	require.NoError(test, err)
	assert.Equal(test, protoCommon.OrderStatus_STATUS_CREATED, ownerResponse.GetStatus())

	_, err = st.OrderClient.GetOrderStatus(
		st.CtxWithUserID(ctx, anotherID),
		&proto.GetOrderStatusRequest{OrderId: createResponse.GetOrderId()},
	)
	require.Error(test, err)
	st2, _ := status.FromError(err)
	assert.Equal(test, codes.NotFound, st2.Code())
}

func TestTimeDoesNotExpireOrder(test *testing.T) {
	ctx, st := suite.New(test)
	st.ClearOrders(ctx)

	orderID := uuid.New()
	userID := uuid.New()

	_, err := st.Pool.Exec(ctx,
		`INSERT INTO orders (id, user_id, market_id, type, price, quantity, status, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		orderID, userID, uuid.New(), 1, "50.00", 1, 1,
		time.Now().UTC().Add(-time.Hour),
	)
	require.NoError(test, err)

	getResponse, err := st.OrderClient.GetOrderStatus(
		st.CtxWithUserID(ctx, userID),
		&proto.GetOrderStatusRequest{OrderId: orderID.String()},
	)
	require.NoError(test, err)
	assert.Equal(test, protoCommon.OrderStatus_STATUS_CREATED, getResponse.GetStatus())
}
