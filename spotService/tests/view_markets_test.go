//go:build integration

package tests

import (
	"fmt"
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

func TestUserSeesOnlyEnabledAndNotDeleted(t *testing.T) {
	ctx, st := suite.New(t)
	st.ClearMarkets(ctx)

	enabledID := uuid.New().String()
	disabledID := uuid.New().String()
	deletedID := uuid.New().String()
	deletedAt := time.Now().UTC()

	st.InsertMarket(ctx, enabledID, "AAA-USDT", true, nil)
	st.InsertMarket(ctx, disabledID, "BBB-USDT", false, nil)
	st.InsertMarket(ctx, deletedID, "CCC-USDT", true, &deletedAt)

	response, err := st.SpotClient.ViewMarkets(
		st.CtxWithRole(ctx, models.UserRoleUser),
		&proto.ViewMarketsRequest{},
	)
	require.NoError(t, err)

	ids := suite.MarketIDs(response)
	assert.Contains(t, ids, enabledID, "user должен видеть enabled рынок")
	assert.NotContains(t, ids, disabledID, "user не должен видеть disabled рынок")
	assert.NotContains(t, ids, deletedID, "user не должен видеть deleted рынок")
	assert.Len(t, ids, 1)
}

func TestViewerSeesNonDeletedIncludingDisabled(t *testing.T) {
	ctx, st := suite.New(t)
	st.ClearMarkets(ctx)

	enabledID := uuid.New().String()
	disabledID := uuid.New().String()
	deletedID := uuid.New().String()
	deletedAt := time.Now().UTC()

	st.InsertMarket(ctx, enabledID, "AAA-USDT", true, nil)
	st.InsertMarket(ctx, disabledID, "BBB-USDT", false, nil)
	st.InsertMarket(ctx, deletedID, "CCC-USDT", true, &deletedAt)

	response, err := st.SpotClient.ViewMarkets(
		st.CtxWithRole(ctx, models.UserRoleViewer),
		&proto.ViewMarketsRequest{},
	)
	require.NoError(t, err)

	ids := suite.MarketIDs(response)
	assert.Contains(t, ids, enabledID, "viewer должен видеть enabled рынок")
	assert.Contains(t, ids, disabledID, "viewer должен видеть disabled рынок")
	assert.NotContains(t, ids, deletedID, "viewer не должен видеть deleted рынок")
	assert.Len(t, ids, 2)
}

func TestAdminSeesEverything(t *testing.T) {
	ctx, st := suite.New(t)
	st.ClearMarkets(ctx)

	enabledID := uuid.New().String()
	disabledID := uuid.New().String()
	deletedEnabledID := uuid.New().String()
	deletedDisabledID := uuid.New().String()
	deletedAt := time.Now().UTC()

	st.InsertMarket(ctx, enabledID, "AAA-USDT", true, nil)
	st.InsertMarket(ctx, disabledID, "BBB-USDT", false, nil)
	st.InsertMarket(ctx, deletedEnabledID, "CCC-USDT", true, &deletedAt)
	st.InsertMarket(ctx, deletedDisabledID, "DDD-USDT", false, &deletedAt)

	response, err := st.SpotClient.ViewMarkets(
		st.CtxWithRole(ctx, models.UserRoleAdmin),
		&proto.ViewMarketsRequest{},
	)
	require.NoError(t, err)

	ids := suite.MarketIDs(response)
	assert.Contains(t, ids, enabledID)
	assert.Contains(t, ids, disabledID)
	assert.Contains(t, ids, deletedEnabledID)
	assert.Contains(t, ids, deletedDisabledID)
	assert.Len(t, ids, 4)
}

func TestMultipleRoles(t *testing.T) {
	ctx, st := suite.New(t)
	st.ClearMarkets(ctx)

	deletedAt := time.Now().UTC()
	deletedID := uuid.New().String()
	enabledID := uuid.New().String()

	st.InsertMarket(ctx, enabledID, "AAA-USDT", true, nil)
	st.InsertMarket(ctx, deletedID, "BBB-USDT", true, &deletedAt)

	response, err := st.SpotClient.ViewMarkets(
		st.CtxWithRole(ctx, models.UserRoleAdmin, models.UserRoleUser),
		&proto.ViewMarketsRequest{},
	)
	require.NoError(t, err)

	ids := suite.MarketIDs(response)
	assert.Contains(t, ids, deletedID, "при наличии роли admin deleted рынок должен быть виден")
	assert.Len(t, ids, 2)
}

func TestSortedByName(t *testing.T) {
	ctx, st := suite.New(t)
	st.ClearMarkets(ctx)

	st.InsertMarket(ctx, uuid.New().String(), "SOL-USDT", true, nil)
	st.InsertMarket(ctx, uuid.New().String(), "BTC-USDT", true, nil)
	st.InsertMarket(ctx, uuid.New().String(), "ETH-USDT", true, nil)

	response, err := st.SpotClient.ViewMarkets(
		st.CtxWithRole(ctx, models.UserRoleUser),
		&proto.ViewMarketsRequest{},
	)
	require.NoError(t, err)
	require.Len(t, response.GetMarkets(), 3)

	assert.Equal(t, "BTC-USDT", response.GetMarkets()[0].GetName())
	assert.Equal(t, "ETH-USDT", response.GetMarkets()[1].GetName())
	assert.Equal(t, "SOL-USDT", response.GetMarkets()[2].GetName())
}

func TestEmptyDB(t *testing.T) {
	ctx, st := suite.New(t)
	st.ClearMarkets(ctx)

	response, err := st.SpotClient.ViewMarkets(
		st.CtxWithRole(ctx, models.UserRoleUser),
		&proto.ViewMarketsRequest{},
	)
	require.Error(t, err)
	assert.Nil(t, response)

	st2, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st2.Code())
}

func TestAllMarketsFilteredOut(t *testing.T) {
	ctx, st := suite.New(t)
	st.ClearMarkets(ctx)

	deletedAt := time.Now().UTC()
	st.InsertMarket(ctx, uuid.New().String(), "AAA-USDT", false, nil)       // disabled
	st.InsertMarket(ctx, uuid.New().String(), "BBB-USDT", true, &deletedAt) // deleted

	response, err := st.SpotClient.ViewMarkets(
		st.CtxWithRole(ctx, models.UserRoleUser),
		&proto.ViewMarketsRequest{},
	)
	require.NoError(t, err)
	assert.Empty(t, response.GetMarkets())
	assert.False(t, response.GetHasMore())
}

func TestPaginationFirstPageHasMore(t *testing.T) {
	ctx, st := suite.New(t)
	st.ClearMarkets(ctx)

	for _, name := range []string{"AAA", "BBB", "CCC", "DDD", "EEE"} {
		st.InsertMarket(ctx, uuid.New().String(), name+"-USDT", true, nil)
	}

	response, err := st.SpotClient.ViewMarkets(
		st.CtxWithRole(ctx, models.UserRoleAdmin),
		&proto.ViewMarketsRequest{Limit: 2, Offset: 0},
	)
	require.NoError(t, err)
	require.Len(t, response.GetMarkets(), 2)
	assert.True(t, response.GetHasMore())
	assert.Equal(t, uint64(2), response.GetNextOffset())
	assert.Equal(t, "AAA-USDT", response.GetMarkets()[0].GetName())
	assert.Equal(t, "BBB-USDT", response.GetMarkets()[1].GetName())
}

func TestPaginationMiddlePage(t *testing.T) {
	ctx, st := suite.New(t)
	st.ClearMarkets(ctx)

	for _, name := range []string{"AAA", "BBB", "CCC", "DDD", "EEE"} {
		st.InsertMarket(ctx, uuid.New().String(), name+"-USDT", true, nil)
	}

	response, err := st.SpotClient.ViewMarkets(
		st.CtxWithRole(ctx, models.UserRoleAdmin),
		&proto.ViewMarketsRequest{Limit: 2, Offset: 2},
	)
	require.NoError(t, err)
	require.Len(t, response.GetMarkets(), 2)
	assert.True(t, response.GetHasMore())
	assert.Equal(t, uint64(4), response.GetNextOffset())
	assert.Equal(t, "CCC-USDT", response.GetMarkets()[0].GetName())
	assert.Equal(t, "DDD-USDT", response.GetMarkets()[1].GetName())
}

func TestPaginationLastPageNoMore(t *testing.T) {
	ctx, st := suite.New(t)
	st.ClearMarkets(ctx)

	for _, name := range []string{"AAA", "BBB", "CCC", "DDD", "EEE"} {
		st.InsertMarket(ctx, uuid.New().String(), name+"-USDT", true, nil)
	}

	response, err := st.SpotClient.ViewMarkets(
		st.CtxWithRole(ctx, models.UserRoleAdmin),
		&proto.ViewMarketsRequest{Limit: 2, Offset: 4},
	)
	require.NoError(t, err)
	require.Len(t, response.GetMarkets(), 1)
	assert.False(t, response.GetHasMore())
	assert.Equal(t, uint64(0), response.GetNextOffset())
	assert.Equal(t, "EEE-USDT", response.GetMarkets()[0].GetName())
}

func TestPaginationOffsetBeyondData(t *testing.T) {
	ctx, st := suite.New(t)
	st.ClearMarkets(ctx)

	st.InsertMarket(ctx, uuid.New().String(), "AAA-USDT", true, nil)
	st.InsertMarket(ctx, uuid.New().String(), "BBB-USDT", true, nil)

	response, err := st.SpotClient.ViewMarkets(
		st.CtxWithRole(ctx, models.UserRoleAdmin),
		// offset больше общего числа маркетов
		&proto.ViewMarketsRequest{Limit: 10, Offset: 100},
	)
	require.NoError(t, err)
	assert.Empty(t, response.GetMarkets())
	assert.False(t, response.GetHasMore())
}

func TestPaginationDefaultLimit(t *testing.T) {
	ctx, st := suite.New(t)
	st.ClearMarkets(ctx)

	for i := 0; i < 5; i++ {
		st.InsertMarket(ctx, uuid.New().String(), fmt.Sprintf("MKT-%02d-USDT", i), true, nil)
	}

	response, err := st.SpotClient.ViewMarkets(
		st.CtxWithRole(ctx, models.UserRoleAdmin),
		&proto.ViewMarketsRequest{Limit: 0, Offset: 0},
	)
	require.NoError(t, err)
	assert.Len(t, response.GetMarkets(), 5)
	assert.False(t, response.GetHasMore())
}

func TestCacheSecondRequestServedFromCache(t *testing.T) {
	ctx, st := suite.New(t)
	st.ClearMarkets(ctx)

	id1 := uuid.New().String()
	id2 := uuid.New().String()
	st.InsertMarket(ctx, id1, "AAA-USDT", true, nil)
	st.InsertMarket(ctx, id2, "BBB-USDT", true, nil)

	firstResponse, err := st.SpotClient.ViewMarkets(
		st.CtxWithRole(ctx, models.UserRoleUser),
		&proto.ViewMarketsRequest{},
	)
	require.NoError(t, err)
	require.Len(t, firstResponse.GetMarkets(), 2)

	secondResponse, err := st.SpotClient.ViewMarkets(
		st.CtxWithRole(ctx, models.UserRoleUser),
		&proto.ViewMarketsRequest{},
	)
	require.NoError(t, err)
	assert.Equal(t, suite.MarketIDs(firstResponse), suite.MarketIDs(secondResponse), "кэш должен возвращать те же данные")
}

func TestCacheStaleDataAfterDirectDBChange(t *testing.T) {
	ctx, st := suite.New(t)
	st.ClearMarkets(ctx)

	id := uuid.New().String()
	st.InsertMarket(ctx, id, "AAA-USDT", true, nil)

	firstResponse, err := st.SpotClient.ViewMarkets(
		st.CtxWithRole(ctx, models.UserRoleAdmin),
		&proto.ViewMarketsRequest{},
	)
	require.NoError(t, err)
	require.Len(t, firstResponse.GetMarkets(), 1)

	st.DeleteMarketFromDB(ctx, id)

	secondResponse, err := st.SpotClient.ViewMarkets(
		st.CtxWithRole(ctx, models.UserRoleAdmin),
		&proto.ViewMarketsRequest{},
	)
	require.NoError(t, err)
	assert.Len(t, secondResponse.GetMarkets(), 1, "кэш ещё не инвалидирован — данные stale")

	st.FlushRedis(ctx)
	thirdResponse, err := st.SpotClient.ViewMarkets(
		st.CtxWithRole(ctx, models.UserRoleAdmin),
		&proto.ViewMarketsRequest{},
	)
	require.Error(t, err, "после flush БД пуста — должен вернуться NotFound")
	assert.Nil(t, thirdResponse)
	st3, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st3.Code())
}

func TestCachePerRoleIsolation(t *testing.T) {
	ctx, st := suite.New(t)
	st.ClearMarkets(ctx)

	disabledID := uuid.New().String()
	st.InsertMarket(ctx, uuid.New().String(), "AAA-USDT", true, nil)
	st.InsertMarket(ctx, disabledID, "BBB-USDT", false, nil)

	responseViewer, err := st.SpotClient.ViewMarkets(
		st.CtxWithRole(ctx, models.UserRoleViewer),
		&proto.ViewMarketsRequest{},
	)
	require.NoError(t, err)
	assert.Len(t, responseViewer.GetMarkets(), 2, "viewer видит 2 рынка (включая disabled)")

	responseUser, err := st.SpotClient.ViewMarkets(
		st.CtxWithRole(ctx, models.UserRoleUser),
		&proto.ViewMarketsRequest{},
	)
	require.NoError(t, err)
	ids := suite.MarketIDs(responseUser)
	assert.NotContains(t, ids, disabledID, "user не должен видеть disabled рынок из viewer-кэша")
	assert.Len(t, responseUser.GetMarkets(), 1)
}

// failed
func TestFieldMappingActiveMarket(t *testing.T) {
	ctx, st := suite.New(t)
	st.ClearMarkets(ctx)

	id := uuid.New().String()
	st.InsertMarket(ctx, id, "BTC-USDT", true, nil)

	response, err := st.SpotClient.ViewMarkets(
		st.CtxWithRole(ctx, models.UserRoleAdmin),
		&proto.ViewMarketsRequest{},
	)
	require.NoError(t, err)
	require.Len(t, response.GetMarkets(), 1)

	m := response.GetMarkets()[0]
	assert.Equal(t, id, m.GetId())
	assert.Equal(t, "BTC-USDT", m.GetName())
	assert.True(t, m.GetEnabled())
	assert.Nil(t, m.GetDeletedAt())
}

func TestFieldMappingDeletedMarket(t *testing.T) {
	ctx, st := suite.New(t)
	st.ClearMarkets(ctx)

	id := uuid.New().String()
	deletedAt := time.Now().UTC().Truncate(time.Millisecond)
	st.InsertMarket(ctx, id, "DOGE-USDT", false, &deletedAt)

	response, err := st.SpotClient.ViewMarkets(
		st.CtxWithRole(ctx, models.UserRoleAdmin),
		&proto.ViewMarketsRequest{},
	)
	require.NoError(t, err)
	require.Len(t, response.GetMarkets(), 1)

	m := response.GetMarkets()[0]
	assert.Equal(t, id, m.GetId())
	assert.False(t, m.GetEnabled())
	assert.NotNil(t, m.GetDeletedAt())
	assert.WithinDuration(t, deletedAt, m.GetDeletedAt().AsTime(), time.Second)
}

func TestNoRolesUnauthenticated(t *testing.T) {
	ctx, st := suite.New(t)
	st.ClearMarkets(ctx)
	st.InsertMarket(ctx, uuid.New().String(), "BTC-USDT", true, nil)

	response, err := st.SpotClient.ViewMarkets(ctx, &proto.ViewMarketsRequest{})
	require.Error(t, err)
	assert.Nil(t, response)

	st2, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unauthenticated, st2.Code())
}

func TestLargeNumberOfMarkets(t *testing.T) {
	ctx, st := suite.New(t)
	st.ClearMarkets(ctx)

	const count = 100
	for i := 0; i < count; i++ {
		st.InsertMarket(ctx, uuid.New().String(), fmt.Sprintf("MKT-%03d-USDT", i), true, nil)
	}

	response, err := st.SpotClient.ViewMarkets(
		st.CtxWithRole(ctx, models.UserRoleAdmin),
		&proto.ViewMarketsRequest{Limit: 0},
	)
	require.NoError(t, err)
	assert.Len(t, response.GetMarkets(), 20, "должен вернуть defaultLimit маркетов")
	assert.True(t, response.GetHasMore())
	assert.Equal(t, uint64(20), response.GetNextOffset())
}

func TestLimitExceedsMaxLimit(t *testing.T) {
	ctx, st := suite.New(t)
	st.ClearMarkets(ctx)

	for i := 0; i < 110; i++ {
		st.InsertMarket(ctx, uuid.New().String(), fmt.Sprintf("MKT-%03d-USDT", i), true, nil)
	}

	response, err := st.SpotClient.ViewMarkets(
		st.CtxWithRole(ctx, models.UserRoleAdmin),
		&proto.ViewMarketsRequest{Limit: 999},
	)
	require.NoError(t, err)
	assert.Len(t, response.GetMarkets(), 100, "limit должен быть ограничен maxLimit")
	assert.True(t, response.GetHasMore())
}

func TestPaginationConsistencyAllPages(t *testing.T) {
	ctx, st := suite.New(t)
	st.ClearMarkets(ctx)

	const total = 7
	for i := 0; i < total; i++ {
		st.InsertMarket(ctx, uuid.New().String(), fmt.Sprintf("MKT-%02d-USDT", i), true, nil)
	}

	const pageSize = uint64(3)
	var allIDs []string
	offset := uint64(0)

	for {
		response, err := st.SpotClient.ViewMarkets(
			st.CtxWithRole(ctx, models.UserRoleAdmin),
			&proto.ViewMarketsRequest{Limit: pageSize, Offset: offset},
		)
		require.NoError(t, err)
		allIDs = append(allIDs, suite.MarketIDs(response)...)

		if !response.GetHasMore() {
			break
		}
		offset = response.GetNextOffset()
	}

	assert.Len(t, allIDs, total, "суммарно должны вернуть все маркеты")

	seen := make(map[string]struct{}, len(allIDs))
	for _, id := range allIDs {
		seen[id] = struct{}{}
	}
	assert.Len(t, seen, total, "дублей в пагинации быть не должно")
}
