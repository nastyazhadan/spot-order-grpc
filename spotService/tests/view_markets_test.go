//go:build integration

package tests

import (
	"testing"
	"time"

	"github.com/brianvoe/gofakeit/v6"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	proto "github.com/nastyazhadan/spot-order-grpc/shared/protos/gen/go/spot/v1"
	"github.com/nastyazhadan/spot-order-grpc/spotService/tests/suite"
)

func init() {
	gofakeit.Seed(time.Now().UnixNano())
}

func TestUserRole(test *testing.T) {
	ctx, st := suite.New(test)
	st.ClearMarkets(ctx)

	enabledID := uuid.New().String()
	disabledID := uuid.New().String()
	deletedID := uuid.New().String()
	deletedTime := time.Now().UTC()

	st.InsertMarket(ctx, enabledID, "BTC-USDT", true, nil)
	st.InsertMarket(ctx, disabledID, "ETH-USDT", false, nil)
	st.InsertMarket(ctx, deletedID, "DOGE-USDT", true, &deletedTime)

	response, err := st.SpotClient.ViewMarkets(ctx, &proto.ViewMarketsRequest{
		UserRoles: []proto.UserRole{proto.UserRole_ROLE_USER},
	})
	require.NoError(test, err)
	require.NotNil(test, response)

	ids := marketIDs(response)
	assert.Contains(test, ids, enabledID, "user должен видеть enabled рынок")
	assert.NotContains(test, ids, disabledID, "user не должен видеть disabled рынок")
	assert.NotContains(test, ids, deletedID, "user не должен видеть deleted рынок")
	assert.Len(test, ids, 1)
}

func TestViewerRole(test *testing.T) {
	ctx, st := suite.New(test)
	st.ClearMarkets(ctx)

	enabledID := uuid.New().String()
	disabledID := uuid.New().String()
	deletedID := uuid.New().String()
	deletedTime := time.Now().UTC()

	st.InsertMarket(ctx, enabledID, "BTC-USDT", true, nil)
	st.InsertMarket(ctx, disabledID, "ETH-USDT", false, nil)
	st.InsertMarket(ctx, deletedID, "DOGE-USDT", true, &deletedTime)

	response, err := st.SpotClient.ViewMarkets(ctx, &proto.ViewMarketsRequest{
		UserRoles: []proto.UserRole{proto.UserRole_ROLE_VIEWER},
	})
	require.NoError(test, err)

	ids := marketIDs(response)
	assert.Contains(test, ids, enabledID, "viewer должен видеть enabled рынок")
	assert.Contains(test, ids, disabledID, "viewer должен видеть disabled рынок")
	assert.NotContains(test, ids, deletedID, "viewer не должен видеть deleted рынок")
	assert.Len(test, ids, 2)
}

func TestAdminRole(test *testing.T) {
	ctx, st := suite.New(test)
	st.ClearMarkets(ctx)

	enabledID := uuid.New().String()
	disabledID := uuid.New().String()
	deletedID := uuid.New().String()
	deletedTime := time.Now().UTC()

	st.InsertMarket(ctx, enabledID, "BTC-USDT", true, nil)
	st.InsertMarket(ctx, disabledID, "ETH-USDT", false, nil)
	st.InsertMarket(ctx, deletedID, "DOGE-USDT", true, &deletedTime)

	response, err := st.SpotClient.ViewMarkets(ctx, &proto.ViewMarketsRequest{
		UserRoles: []proto.UserRole{proto.UserRole_ROLE_ADMIN},
	})
	require.NoError(test, err)

	ids := marketIDs(response)
	assert.Contains(test, ids, enabledID, "admin должен видеть enabled рынок")
	assert.Contains(test, ids, disabledID, "admin должен видеть disabled рынок")
	assert.Contains(test, ids, deletedID, "admin должен видеть deleted рынок")
	assert.Len(test, ids, 3)
}

func TestMarketsSortedByName(test *testing.T) {
	ctx, st := suite.New(test)
	st.ClearMarkets(ctx)

	st.InsertMarket(ctx, uuid.New().String(), "SOL-USDT", true, nil)
	st.InsertMarket(ctx, uuid.New().String(), "BTC-USDT", true, nil)
	st.InsertMarket(ctx, uuid.New().String(), "ETH-USDT", true, nil)

	response, err := st.SpotClient.ViewMarkets(ctx, &proto.ViewMarketsRequest{
		UserRoles: []proto.UserRole{proto.UserRole_ROLE_USER},
	})
	require.NoError(test, err)
	require.Len(test, response.GetMarkets(), 3)

	assert.Equal(test, "BTC-USDT", response.GetMarkets()[0].GetName())
	assert.Equal(test, "ETH-USDT", response.GetMarkets()[1].GetName())
	assert.Equal(test, "SOL-USDT", response.GetMarkets()[2].GetName())
}

func TestAdminAndUserRoles(test *testing.T) {
	ctx, st := suite.New(test)
	st.ClearMarkets(ctx)

	deletedTime := time.Now().UTC()
	deletedID := uuid.New().String()

	st.InsertMarket(ctx, uuid.New().String(), "BTC-USDT", true, nil)
	st.InsertMarket(ctx, deletedID, "DOGE-USDT", true, &deletedTime)

	response, err := st.SpotClient.ViewMarkets(ctx, &proto.ViewMarketsRequest{
		UserRoles: []proto.UserRole{proto.UserRole_ROLE_ADMIN, proto.UserRole_ROLE_USER},
	})
	require.NoError(test, err)

	ids := marketIDs(response)
	assert.Contains(test, ids, deletedID, "при наличии роли admin удалённые рынки должны быть видны")
	assert.Len(test, ids, 2)
}

func TestEmptyTableReturnsNotFound(test *testing.T) {
	ctx, st := suite.New(test)
	st.ClearMarkets(ctx)

	response, err := st.SpotClient.ViewMarkets(ctx, &proto.ViewMarketsRequest{
		UserRoles: []proto.UserRole{proto.UserRole_ROLE_USER},
	})
	require.Error(test, err)
	assert.Nil(test, response)

	st2, ok := status.FromError(err)
	require.True(test, ok)
	assert.Equal(test, codes.NotFound, st2.Code())
}

func TestAllMarketsDisabledOrDeletedReturnsEmptyList(test *testing.T) {
	ctx, st := suite.New(test)
	st.ClearMarkets(ctx)

	deletedTime := time.Now().UTC()
	st.InsertMarket(ctx, uuid.New().String(), "ETH-USDT", false, nil)
	st.InsertMarket(ctx, uuid.New().String(), "DOGE-USDT", true, &deletedTime)

	response, err := st.SpotClient.ViewMarkets(ctx, &proto.ViewMarketsRequest{
		UserRoles: []proto.UserRole{proto.UserRole_ROLE_USER},
	})
	require.NoError(test, err)
	assert.Empty(test, response.GetMarkets())
}

func TestLargeNumberOfMarkets(test *testing.T) {
	ctx, st := suite.New(test)
	st.ClearMarkets(ctx)

	const count = 100
	for i := 0; i < count; i++ {
		st.InsertMarket(ctx, uuid.New().String(), gofakeit.Company(), i%2 == 0, nil)
	}

	response, err := st.SpotClient.ViewMarkets(ctx, &proto.ViewMarketsRequest{
		UserRoles: []proto.UserRole{proto.UserRole_ROLE_ADMIN},
	})
	require.NoError(test, err)
	assert.Len(test, response.GetMarkets(), count)
}

func TestMarketDeletedNotVisibleToUserAndViewer(test *testing.T) {
	ctx, st := suite.New(test)
	st.ClearMarkets(ctx)

	now := time.Now().UTC()
	id := uuid.New().String()
	st.InsertMarket(ctx, id, "EDGE-USDT", true, &now)

	response, err := st.SpotClient.ViewMarkets(ctx, &proto.ViewMarketsRequest{
		UserRoles: []proto.UserRole{proto.UserRole_ROLE_USER, proto.UserRole_ROLE_VIEWER},
	})
	require.NoError(test, err)
	assert.NotContains(test, marketIDs(response), id)
}

func TestFailCases(test *testing.T) {
	ctx, st := suite.New(test)

	tests := []struct {
		name         string
		request      *proto.ViewMarketsRequest
		expectedCode codes.Code
	}{
		{
			name:         "пустой список ролей",
			request:      &proto.ViewMarketsRequest{UserRoles: []proto.UserRole{}},
			expectedCode: codes.InvalidArgument,
		},
		{
			name:         "nil UserRoles",
			request:      &proto.ViewMarketsRequest{},
			expectedCode: codes.InvalidArgument,
		},
		{
			name: "роль UNSPECIFIED",
			request: &proto.ViewMarketsRequest{
				UserRoles: []proto.UserRole{proto.UserRole_ROLE_UNSPECIFIED},
			},
			expectedCode: codes.InvalidArgument,
		},
		{
			name: "дубликат роли",
			request: &proto.ViewMarketsRequest{
				UserRoles: []proto.UserRole{proto.UserRole_ROLE_USER, proto.UserRole_ROLE_USER},
			},
			expectedCode: codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		test.Run(tt.name, func(t *testing.T) {
			response, err := st.SpotClient.ViewMarkets(ctx, tt.request)
			require.Error(t, err)
			assert.Nil(t, response)

			stat, ok := status.FromError(err)
			require.True(t, ok)
			assert.Equal(t, tt.expectedCode, stat.Code(), "неожиданный gRPC код для кейса %q", tt.name)
		})
	}
}

func marketIDs(response *proto.ViewMarketsResponse) []string {
	if response == nil {
		return nil
	}

	ids := make([]string, 0, len(response.GetMarkets()))
	for _, market := range response.GetMarkets() {
		ids = append(ids, market.GetId())
	}

	return ids
}
