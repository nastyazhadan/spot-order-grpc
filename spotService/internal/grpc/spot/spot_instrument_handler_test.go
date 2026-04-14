package spot

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	proto "github.com/nastyazhadan/spot-order-grpc/protos/gen/go/spot/v1"
	sharedErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors"
	serviceErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/service"
	"github.com/nastyazhadan/spot-order-grpc/shared/models"
	"github.com/nastyazhadan/spot-order-grpc/spotService/internal/grpc/mocks"
)

func newServer(svc *mocks.SpotInstrument) *serverAPI {
	return &serverAPI{spotInstrument: svc}
}

func assertGRPCCode(t *testing.T, err error, wantCode codes.Code) {
	t.Helper()
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok, "ожидался gRPC status error, получен: %T: %v", err, err)
	assert.Equal(t, wantCode, st.Code())
}

func TestViewMarkets(t *testing.T) {
	deletedAt := time.Now().UTC()

	activeMarket := models.Market{
		ID:        uuid.New(),
		Name:      "BTC/USD",
		Enabled:   true,
		UpdatedAt: time.Now().UTC(),
	}
	disabledMarket := models.Market{
		ID:        uuid.New(),
		Name:      "ETH/USD",
		Enabled:   false,
		UpdatedAt: time.Now().UTC(),
	}
	deletedMarket := models.Market{
		ID:        uuid.New(),
		Name:      "XRP/USD",
		Enabled:   true,
		DeletedAt: &deletedAt,
		UpdatedAt: time.Now().UTC(),
	}

	tests := []struct {
		name       string
		request    *proto.ViewMarketsRequest
		setupMocks func(*mocks.SpotInstrument)
		checkResp  func(t *testing.T, resp *proto.ViewMarketsResponse)
		checkErr   func(t *testing.T, err error)
	}{
		{
			name:       "nil request — InvalidArgument",
			request:    nil,
			setupMocks: func(_ *mocks.SpotInstrument) {},
			checkErr: func(t *testing.T, err error) {
				assertGRPCCode(t, err, codes.InvalidArgument)
			},
		},
		{
			name:    "limit=0, offset=0 — сервис вызывается с нулями (defaults на стороне сервиса)",
			request: &proto.ViewMarketsRequest{Limit: 0, Offset: 0},
			setupMocks: func(svc *mocks.SpotInstrument) {
				svc.On("ViewMarkets", mock.Anything, uint64(0), uint64(0)).
					Return([]models.Market{activeMarket}, uint64(0), false, nil)
			},
			checkResp: func(t *testing.T, resp *proto.ViewMarketsResponse) {
				require.NotNil(t, resp)
				assert.Len(t, resp.GetMarkets(), 1)
				assert.False(t, resp.GetHasMore())
				assert.Equal(t, uint64(0), resp.GetNextOffset())
			},
		},
		{
			name:    "limit=10, offset=20 — сервис вызывается с этими значениями",
			request: &proto.ViewMarketsRequest{Limit: 10, Offset: 20},
			setupMocks: func(svc *mocks.SpotInstrument) {
				svc.On("ViewMarkets", mock.Anything, uint64(10), uint64(20)).
					Return([]models.Market{activeMarket}, uint64(30), true, nil)
			},
			checkResp: func(t *testing.T, resp *proto.ViewMarketsResponse) {
				require.NotNil(t, resp)
				assert.True(t, resp.GetHasMore())
				assert.Equal(t, uint64(30), resp.GetNextOffset())
			},
		},
		{
			name:    "hasMore=true — nextOffset и hasMore пробрасываются в ответ",
			request: &proto.ViewMarketsRequest{Limit: 5, Offset: 0},
			setupMocks: func(svc *mocks.SpotInstrument) {
				svc.On("ViewMarkets", mock.Anything, uint64(5), uint64(0)).
					Return([]models.Market{activeMarket, disabledMarket}, uint64(5), true, nil)
			},
			checkResp: func(t *testing.T, resp *proto.ViewMarketsResponse) {
				require.NotNil(t, resp)
				assert.True(t, resp.GetHasMore())
				assert.Equal(t, uint64(5), resp.GetNextOffset())
				assert.Len(t, resp.GetMarkets(), 2)
			},
		},
		{
			name:    "hasMore=false — nextOffset=0 в ответе",
			request: &proto.ViewMarketsRequest{Limit: 5, Offset: 0},
			setupMocks: func(svc *mocks.SpotInstrument) {
				svc.On("ViewMarkets", mock.Anything, uint64(5), uint64(0)).
					Return([]models.Market{activeMarket}, uint64(0), false, nil)
			},
			checkResp: func(t *testing.T, resp *proto.ViewMarketsResponse) {
				require.NotNil(t, resp)
				assert.False(t, resp.GetHasMore())
				assert.Equal(t, uint64(0), resp.GetNextOffset())
			},
		},
		{
			name:    "пустой список маркетов — ответ с пустым массивом, не nil",
			request: &proto.ViewMarketsRequest{Limit: 10, Offset: 0},
			setupMocks: func(svc *mocks.SpotInstrument) {
				svc.On("ViewMarkets", mock.Anything, uint64(10), uint64(0)).
					Return([]models.Market{}, uint64(0), false, nil)
			},
			checkResp: func(t *testing.T, resp *proto.ViewMarketsResponse) {
				require.NotNil(t, resp)
				assert.Empty(t, resp.GetMarkets())
				assert.False(t, resp.GetHasMore())
			},
		},
		{
			name:    "активный маркет — все поля маппятся корректно",
			request: &proto.ViewMarketsRequest{Limit: 1, Offset: 0},
			setupMocks: func(svc *mocks.SpotInstrument) {
				market := models.Market{
					ID:        uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
					Name:      "BTC/USD",
					Enabled:   true,
					DeletedAt: nil,
					UpdatedAt: time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC),
				}
				svc.On("ViewMarkets", mock.Anything, uint64(1), uint64(0)).
					Return([]models.Market{market}, uint64(0), false, nil)
			},
			checkResp: func(t *testing.T, resp *proto.ViewMarketsResponse) {
				require.NotNil(t, resp)
				require.Len(t, resp.GetMarkets(), 1)
				m := resp.GetMarkets()[0]
				assert.Equal(t, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", m.GetId())
				assert.Equal(t, "BTC/USD", m.GetName())
				assert.True(t, m.GetEnabled())
				assert.Nil(t, m.GetDeletedAt())
				assert.NotNil(t, m.GetUpdatedAt())
			},
		},
		{
			name:    "маркет с DeletedAt — deletedAt маппится в proto timestamp",
			request: &proto.ViewMarketsRequest{Limit: 1, Offset: 0},
			setupMocks: func(svc *mocks.SpotInstrument) {
				svc.On("ViewMarkets", mock.Anything, uint64(1), uint64(0)).
					Return([]models.Market{deletedMarket}, uint64(0), false, nil)
			},
			checkResp: func(t *testing.T, resp *proto.ViewMarketsResponse) {
				require.NotNil(t, resp)
				require.Len(t, resp.GetMarkets(), 1)
				m := resp.GetMarkets()[0]
				assert.NotNil(t, m.GetDeletedAt())
				assert.Equal(t, deletedMarket.ID.String(), m.GetId())
			},
		},
		{
			name:    "маркет с нулевым UpdatedAt — updatedAt в proto равен nil",
			request: &proto.ViewMarketsRequest{Limit: 1, Offset: 0},
			setupMocks: func(svc *mocks.SpotInstrument) {
				market := models.Market{
					ID: uuid.New(), Name: "test", Enabled: true, UpdatedAt: time.Time{},
				}
				svc.On("ViewMarkets", mock.Anything, uint64(1), uint64(0)).
					Return([]models.Market{market}, uint64(0), false, nil)
			},
			checkResp: func(t *testing.T, resp *proto.ViewMarketsResponse) {
				require.Len(t, resp.GetMarkets(), 1)
				assert.Nil(t, resp.GetMarkets()[0].GetUpdatedAt())
			},
		},
		{
			name:    "несколько маркетов — порядок сохраняется",
			request: &proto.ViewMarketsRequest{Limit: 10, Offset: 0},
			setupMocks: func(svc *mocks.SpotInstrument) {
				markets := []models.Market{activeMarket, disabledMarket, deletedMarket}
				svc.On("ViewMarkets", mock.Anything, uint64(10), uint64(0)).
					Return(markets, uint64(0), false, nil)
			},
			checkResp: func(t *testing.T, resp *proto.ViewMarketsResponse) {
				require.Len(t, resp.GetMarkets(), 3)
				assert.Equal(t, activeMarket.ID.String(), resp.GetMarkets()[0].GetId())
				assert.Equal(t, disabledMarket.ID.String(), resp.GetMarkets()[1].GetId())
				assert.Equal(t, deletedMarket.ID.String(), resp.GetMarkets()[2].GetId())
			},
		},
		{
			name:    "сервис возвращает ошибку — хендлер пробрасывает её без изменений",
			request: &proto.ViewMarketsRequest{Limit: 10, Offset: 0},
			setupMocks: func(svc *mocks.SpotInstrument) {
				svc.On("ViewMarkets", mock.Anything, uint64(10), uint64(0)).
					Return(nil, uint64(0), false, serviceErrors.ErrMarketsNotFound)
			},
			checkErr: func(t *testing.T, err error) {
				require.Error(t, err)
				assert.ErrorIs(t, err, serviceErrors.ErrMarketsNotFound)
			},
		},
		{
			name:    "сервис возвращает ErrInvalidPagination — хендлер пробрасывает",
			request: &proto.ViewMarketsRequest{Limit: 10, Offset: 0},
			setupMocks: func(svc *mocks.SpotInstrument) {
				svc.On("ViewMarkets", mock.Anything, uint64(10), uint64(0)).
					Return(nil, uint64(0), false, serviceErrors.ErrInvalidPagination)
			},
			checkErr: func(t *testing.T, err error) {
				require.Error(t, err)
				assert.ErrorIs(t, err, serviceErrors.ErrInvalidPagination)
			},
		},
		{
			name:    "сервис возвращает неизвестную ошибку — хендлер пробрасывает",
			request: &proto.ViewMarketsRequest{Limit: 10, Offset: 0},
			setupMocks: func(svc *mocks.SpotInstrument) {
				svc.On("ViewMarkets", mock.Anything, uint64(10), uint64(0)).
					Return(nil, uint64(0), false, errors.New("internal db error"))
			},
			checkErr: func(t *testing.T, err error) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "internal db error")
			},
		},
		{
			name:    "сервис возвращает gRPC status error — пробрасывается как есть",
			request: &proto.ViewMarketsRequest{Limit: 10, Offset: 0},
			setupMocks: func(svc *mocks.SpotInstrument) {
				svc.On("ViewMarkets", mock.Anything, uint64(10), uint64(0)).
					Return(nil, uint64(0), false, status.Error(codes.Unavailable, "market unavailable"))
			},
			checkErr: func(t *testing.T, err error) {
				assertGRPCCode(t, err, codes.Unavailable)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := mocks.NewSpotInstrument(t)
			tt.setupMocks(svc)

			server := newServer(svc)
			resp, err := server.ViewMarkets(context.Background(), tt.request)

			if tt.checkErr != nil {
				tt.checkErr(t, err)
				assert.Nil(t, resp)
			} else {
				require.NoError(t, err)
				if tt.checkResp != nil {
					tt.checkResp(t, resp)
				}
			}
		})
	}
}

func TestGetMarketByID(t *testing.T) {
	validID := uuid.New()
	deletedAt := time.Now().UTC()

	activeMarket := models.Market{
		ID:        validID,
		Name:      "BTC/USD",
		Enabled:   true,
		DeletedAt: nil,
		UpdatedAt: time.Date(2025, 6, 1, 10, 0, 0, 0, time.UTC),
	}

	tests := []struct {
		name       string
		request    *proto.GetMarketByIDRequest
		setupMocks func(*mocks.SpotInstrument)
		checkResp  func(t *testing.T, resp *proto.GetMarketByIDResponse)
		checkErr   func(t *testing.T, err error)
	}{
		{
			name:       "nil request — InvalidArgument",
			request:    nil,
			setupMocks: func(_ *mocks.SpotInstrument) {},
			checkErr: func(t *testing.T, err error) {
				assertGRPCCode(t, err, codes.InvalidArgument)
			},
		},
		{
			name:       "пустой market_id — InvalidArgument (uuid.Parse fails)",
			request:    &proto.GetMarketByIDRequest{MarketId: ""},
			setupMocks: func(_ *mocks.SpotInstrument) {},
			checkErr: func(t *testing.T, err error) {
				assertGRPCCode(t, err, codes.InvalidArgument)
			},
		},
		{
			name:       "невалидный UUID — InvalidArgument",
			request:    &proto.GetMarketByIDRequest{MarketId: "not-a-uuid"},
			setupMocks: func(_ *mocks.SpotInstrument) {},
			checkErr: func(t *testing.T, err error) {
				assertGRPCCode(t, err, codes.InvalidArgument)
			},
		},
		{
			name:       "UUID с лишними символами — InvalidArgument",
			request:    &proto.GetMarketByIDRequest{MarketId: validID.String() + "extra"},
			setupMocks: func(_ *mocks.SpotInstrument) {},
			checkErr: func(t *testing.T, err error) {
				assertGRPCCode(t, err, codes.InvalidArgument)
			},
		},
		{
			name:    "валидный UUID — сервис вызывается с распарсенным uuid.UUID",
			request: &proto.GetMarketByIDRequest{MarketId: validID.String()},
			setupMocks: func(svc *mocks.SpotInstrument) {
				svc.On("GetMarketByID", mock.Anything, validID).
					Return(activeMarket, nil)
			},
			checkResp: func(t *testing.T, resp *proto.GetMarketByIDResponse) {
				require.NotNil(t, resp)
				require.NotNil(t, resp.GetMarket())
			},
		},
		{
			name:    "активный маркет — все поля маппятся корректно",
			request: &proto.GetMarketByIDRequest{MarketId: validID.String()},
			setupMocks: func(svc *mocks.SpotInstrument) {
				svc.On("GetMarketByID", mock.Anything, validID).
					Return(activeMarket, nil)
			},
			checkResp: func(t *testing.T, resp *proto.GetMarketByIDResponse) {
				require.NotNil(t, resp)
				m := resp.GetMarket()
				assert.Equal(t, validID.String(), m.GetId())
				assert.Equal(t, "BTC/USD", m.GetName())
				assert.True(t, m.GetEnabled())
				assert.Nil(t, m.GetDeletedAt())
				assert.NotNil(t, m.GetUpdatedAt())
			},
		},
		{
			name:    "маркет с DeletedAt — deletedAt маппится в proto timestamp",
			request: &proto.GetMarketByIDRequest{MarketId: validID.String()},
			setupMocks: func(svc *mocks.SpotInstrument) {
				market := models.Market{
					ID:        validID,
					Name:      "Deleted Market",
					Enabled:   false,
					DeletedAt: &deletedAt,
					UpdatedAt: time.Now().UTC(),
				}
				svc.On("GetMarketByID", mock.Anything, validID).
					Return(market, nil)
			},
			checkResp: func(t *testing.T, resp *proto.GetMarketByIDResponse) {
				require.NotNil(t, resp)
				m := resp.GetMarket()
				assert.Equal(t, validID.String(), m.GetId())
				assert.False(t, m.GetEnabled())
				assert.NotNil(t, m.GetDeletedAt())
				assert.WithinDuration(t, deletedAt, m.GetDeletedAt().AsTime(), time.Second)
			},
		},
		{
			name:    "маркет с нулевым UpdatedAt — updatedAt в proto равен nil",
			request: &proto.GetMarketByIDRequest{MarketId: validID.String()},
			setupMocks: func(svc *mocks.SpotInstrument) {
				market := models.Market{
					ID: validID, Name: "test", Enabled: true, UpdatedAt: time.Time{},
				}
				svc.On("GetMarketByID", mock.Anything, validID).
					Return(market, nil)
			},
			checkResp: func(t *testing.T, resp *proto.GetMarketByIDResponse) {
				require.NotNil(t, resp)
				assert.Nil(t, resp.GetMarket().GetUpdatedAt())
			},
		},
		{
			name:    "сервис возвращает ErrMarketNotFound — хендлер пробрасывает",
			request: &proto.GetMarketByIDRequest{MarketId: validID.String()},
			setupMocks: func(svc *mocks.SpotInstrument) {
				svc.On("GetMarketByID", mock.Anything, validID).
					Return(models.Market{}, sharedErrors.ErrMarketNotFound{ID: validID})
			},
			checkErr: func(t *testing.T, err error) {
				require.Error(t, err)
				var notFound sharedErrors.ErrMarketNotFound
				assert.ErrorAs(t, err, &notFound)
				assert.Equal(t, validID, notFound.ID)
			},
		},
		{
			name:    "сервис возвращает ErrDisabled — хендлер пробрасывает",
			request: &proto.GetMarketByIDRequest{MarketId: validID.String()},
			setupMocks: func(svc *mocks.SpotInstrument) {
				svc.On("GetMarketByID", mock.Anything, validID).
					Return(models.Market{}, serviceErrors.ErrDisabled{ID: validID})
			},
			checkErr: func(t *testing.T, err error) {
				require.Error(t, err)
				var disabled serviceErrors.ErrDisabled
				assert.ErrorAs(t, err, &disabled)
				assert.Equal(t, validID, disabled.ID)
			},
		},
		{
			name:    "сервис возвращает неизвестную ошибку — хендлер пробрасывает",
			request: &proto.GetMarketByIDRequest{MarketId: validID.String()},
			setupMocks: func(svc *mocks.SpotInstrument) {
				svc.On("GetMarketByID", mock.Anything, validID).
					Return(models.Market{}, errors.New("db timeout"))
			},
			checkErr: func(t *testing.T, err error) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "db timeout")
			},
		},
		{
			name:    "сервис возвращает gRPC status error — пробрасывается как есть",
			request: &proto.GetMarketByIDRequest{MarketId: validID.String()},
			setupMocks: func(svc *mocks.SpotInstrument) {
				svc.On("GetMarketByID", mock.Anything, validID).
					Return(models.Market{}, status.Error(codes.Unavailable, "circuit breaker open"))
			},
			checkErr: func(t *testing.T, err error) {
				assertGRPCCode(t, err, codes.Unavailable)
			},
		},
		{
			name:    "UUID в верхнем регистре — успешно парсится",
			request: &proto.GetMarketByIDRequest{MarketId: validID.String()},
			setupMocks: func(svc *mocks.SpotInstrument) {
				svc.On("GetMarketByID", mock.Anything, validID).
					Return(activeMarket, nil)
			},
			checkResp: func(t *testing.T, resp *proto.GetMarketByIDResponse) {
				require.NotNil(t, resp.GetMarket())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := mocks.NewSpotInstrument(t)
			tt.setupMocks(svc)

			server := newServer(svc)
			resp, err := server.GetMarketByID(context.Background(), tt.request)

			if tt.checkErr != nil {
				tt.checkErr(t, err)
				assert.Nil(t, resp)
			} else {
				require.NoError(t, err)
				if tt.checkResp != nil {
					tt.checkResp(t, resp)
				}
			}
		})
	}
}
