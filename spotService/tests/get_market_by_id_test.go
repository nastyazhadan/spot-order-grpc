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

	proto "github.com/nastyazhadan/spot-order-grpc/protos/gen/go/spot/v1"
	"github.com/nastyazhadan/spot-order-grpc/shared/models"
	"github.com/nastyazhadan/spot-order-grpc/spotService/tests/suite"
)

func TestAdminSeesAnyMarket(t *testing.T) {
	ctx, st := suite.New(t)
	st.ClearMarkets(ctx)

	deletedAt := time.Now().UTC()
	cases := []struct {
		name      string
		enabled   bool
		deletedAt *time.Time
	}{
		{"BTC-USDT", true, nil},
		{"ETH-USDT", false, nil},
		{"SOL-USDT", true, &deletedAt},
		{"DOGE-USDT", false, &deletedAt},
	}

	for _, tc := range cases {
		id := uuid.New().String()
		st.InsertMarket(ctx, id, tc.name, tc.enabled, tc.deletedAt)

		response, err := st.SpotClient.GetMarketByID(
			st.CtxWithRole(ctx, models.UserRoleAdmin),
			&proto.GetMarketByIDRequest{MarketId: id},
		)
		require.NoError(t, err, "admin должен видеть маркет %q", tc.name)
		assert.Equal(t, id, response.GetMarket().GetId())
	}
}

func TestViewerCannotSeeDeletedMarket(t *testing.T) {
	ctx, st := suite.New(t)
	st.ClearMarkets(ctx)

	deletedAt := time.Now().UTC()
	activeID := uuid.New().String()
	deletedID := uuid.New().String()
	disabledID := uuid.New().String()

	st.InsertMarket(ctx, activeID, "AAA-USDT", true, nil)
	st.InsertMarket(ctx, deletedID, "BBB-USDT", true, &deletedAt)
	st.InsertMarket(ctx, disabledID, "CCC-USDT", false, nil)

	response, err := st.SpotClient.GetMarketByID(
		st.CtxWithRole(ctx, models.UserRoleViewer),
		&proto.GetMarketByIDRequest{MarketId: activeID},
	)
	require.NoError(t, err)
	assert.Equal(t, activeID, response.GetMarket().GetId())

	response, err = st.SpotClient.GetMarketByID(
		st.CtxWithRole(ctx, models.UserRoleViewer),
		&proto.GetMarketByIDRequest{MarketId: disabledID},
	)
	require.NoError(t, err)
	assert.Equal(t, disabledID, response.GetMarket().GetId())

	response, err = st.SpotClient.GetMarketByID(
		st.CtxWithRole(ctx, models.UserRoleViewer),
		&proto.GetMarketByIDRequest{MarketId: deletedID},
	)
	require.Error(t, err)
	assert.Nil(t, response)
	st2, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st2.Code())
}

func TestUserCannotSeeDisabledOrDeleted(t *testing.T) {
	ctx, st := suite.New(t)
	st.ClearMarkets(ctx)

	deletedAt := time.Now().UTC()
	activeID := uuid.New().String()
	disabledID := uuid.New().String()
	deletedID := uuid.New().String()
	deletedDisabledID := uuid.New().String()

	st.InsertMarket(ctx, activeID, "AAA-USDT", true, nil)
	st.InsertMarket(ctx, disabledID, "BBB-USDT", false, nil)
	st.InsertMarket(ctx, deletedID, "CCC-USDT", true, &deletedAt)
	st.InsertMarket(ctx, deletedDisabledID, "DDD-USDT", false, &deletedAt)

	response, err := st.SpotClient.GetMarketByID(
		st.CtxWithRole(ctx, models.UserRoleUser),
		&proto.GetMarketByIDRequest{MarketId: activeID},
	)
	require.NoError(t, err)
	assert.Equal(t, activeID, response.GetMarket().GetId())

	_, err = st.SpotClient.GetMarketByID(
		st.CtxWithRole(ctx, models.UserRoleUser),
		&proto.GetMarketByIDRequest{MarketId: disabledID},
	)
	require.Error(t, err)
	st2, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.FailedPrecondition, st2.Code(), "disabled маркет → FailedPrecondition")

	for _, id := range []string{deletedID, deletedDisabledID} {
		_, err = st.SpotClient.GetMarketByID(
			st.CtxWithRole(ctx, models.UserRoleUser),
			&proto.GetMarketByIDRequest{MarketId: id},
		)
		require.Error(t, err)
		st3, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.NotFound, st3.Code(), "deleted маркет %s → NotFound", id)
	}
}

func TestNotExistingNotFound(t *testing.T) {
	ctx, st := suite.New(t)
	st.ClearMarkets(ctx)

	response, err := st.SpotClient.GetMarketByID(
		st.CtxWithRole(ctx, models.UserRoleAdmin),
		&proto.GetMarketByIDRequest{MarketId: uuid.New().String()},
	)
	require.Error(t, err)
	assert.Nil(t, response)

	st2, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st2.Code())
}

func TestFieldMapping(t *testing.T) {
	ctx, st := suite.New(t)
	st.ClearMarkets(ctx)

	id := uuid.New().String()
	deletedAt := time.Now().UTC().Truncate(time.Millisecond)
	st.InsertMarket(ctx, id, "ETH-USDT", false, &deletedAt)

	response, err := st.SpotClient.GetMarketByID(
		st.CtxWithRole(ctx, models.UserRoleAdmin),
		&proto.GetMarketByIDRequest{MarketId: id},
	)
	require.NoError(t, err)

	m := response.GetMarket()
	assert.Equal(t, id, m.GetId())
	assert.Equal(t, "ETH-USDT", m.GetName())
	assert.False(t, m.GetEnabled())
	assert.NotNil(t, m.GetDeletedAt())
	assert.WithinDuration(t, deletedAt, m.GetDeletedAt().AsTime(), time.Second)
	assert.NotNil(t, m.GetUpdatedAt(), "updated_at должен быть выставлен БД")
}

func TestValidationFailCases(t *testing.T) {
	ctx, st := suite.New(t)

	cases := []struct {
		name     string
		marketID string
	}{
		{"пустой market_id", ""},
		{"невалидный UUID", "not-a-uuid"},
		{"UUID с лишними символами", uuid.New().String() + "-extra"},
		{"случайная строка", "just-some-string"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			response, err := st.SpotClient.GetMarketByID(
				st.CtxWithRole(ctx, models.UserRoleAdmin),
				&proto.GetMarketByIDRequest{MarketId: tc.marketID},
			)
			require.Error(t, err)
			assert.Nil(t, response)

			st2, ok := status.FromError(err)
			require.True(t, ok)
			assert.Equal(t, codes.InvalidArgument, st2.Code(),
				"невалидный market_id %q должен давать InvalidArgument", tc.marketID)
		})
	}
}

func TestRoleAccessAdminSeesDeletedDisabled(t *testing.T) {
	ctx, st := suite.New(t)
	st.ClearMarkets(ctx)

	deletedAt := time.Now().UTC()
	id := uuid.New().String()
	st.InsertMarket(ctx, id, "DEAD-USDT", false, &deletedAt)

	response, err := st.SpotClient.GetMarketByID(
		st.CtxWithRole(ctx, models.UserRoleAdmin),
		&proto.GetMarketByIDRequest{MarketId: id},
	)
	require.NoError(t, err)
	m := response.GetMarket()
	assert.Equal(t, id, m.GetId())
	assert.False(t, m.GetEnabled())
	assert.NotNil(t, m.GetDeletedAt())
}
