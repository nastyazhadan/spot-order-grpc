package spot

import (
	"context"
	"errors"
	"testing"
	"time"

	proto "github.com/nastyazhadan/spot-order-grpc/protos/gen/go/spot/v1"
	serviceErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/service"
	"github.com/nastyazhadan/spot-order-grpc/shared/models"
	"github.com/nastyazhadan/spot-order-grpc/spotService/internal/grpc/mocks"

	"github.com/brianvoe/gofakeit/v6"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestViewMarkets(t *testing.T) {
	gofakeit.Seed(0)

	enabledMarket := models.Market{
		ID:        uuid.New(),
		Name:      "BTC/USD",
		Enabled:   true,
		DeletedAt: nil,
	}

	disabledMarket := models.Market{
		ID:        uuid.New(),
		Name:      "ETH/USD",
		Enabled:   false,
		DeletedAt: nil,
	}

	deletedTime := time.Now().UTC()
	deletedMarket := models.Market{
		ID:        uuid.New(),
		Name:      "XRP/USD",
		Enabled:   true,
		DeletedAt: &deletedTime,
	}

	tests := []struct {
		name          string
		request       *proto.ViewMarketsRequest
		setupMocks    func(*mocks.SpotInstrument)
		expectedCode  codes.Code
		checkResponse func(t *testing.T, resp *proto.ViewMarketsResponse)
	}{
		{
			name: "успешное получение рынков - Admin",
			request: &proto.ViewMarketsRequest{
				UserRoles: []proto.UserRole{proto.UserRole_ROLE_ADMIN},
			},
			setupMocks: func(mockSpot *mocks.SpotInstrument) {
				markets := []models.Market{enabledMarket, disabledMarket, deletedMarket}
				mockSpot.On("ViewMarkets",
					mock.Anything,
					[]models.UserRole{models.UserRoleAdmin},
				).Return(markets, nil)
			},
			expectedCode: codes.OK,
			checkResponse: func(t *testing.T, resp *proto.ViewMarketsResponse) {
				assert.NotNil(t, resp)
				assert.Len(t, resp.GetMarkets(), 3)

				names := make([]string, len(resp.GetMarkets()))
				for id, market := range resp.GetMarkets() {
					names[id] = market.GetName()
				}

				assert.True(t, isSorted(names), "markets should be sorted by name")
			},
		},
		{
			name: "успешное получение доступных рынков - User",
			request: &proto.ViewMarketsRequest{
				UserRoles: []proto.UserRole{proto.UserRole_ROLE_USER},
			},
			setupMocks: func(mockSpot *mocks.SpotInstrument) {
				markets := []models.Market{enabledMarket}
				mockSpot.On("ViewMarkets",
					mock.Anything,
					[]models.UserRole{models.UserRoleUser},
				).Return(markets, nil)
			},
			expectedCode: codes.OK,
			checkResponse: func(t *testing.T, resp *proto.ViewMarketsResponse) {
				assert.NotNil(t, resp)
				assert.Len(t, resp.GetMarkets(), 1)
				assert.Equal(t, "BTC/USD", resp.GetMarkets()[0].GetName())
			},
		},
		{
			name: "успешное получение доступных рынков - Viewer",
			request: &proto.ViewMarketsRequest{
				UserRoles: []proto.UserRole{proto.UserRole_ROLE_VIEWER},
			},
			setupMocks: func(mockSpot *mocks.SpotInstrument) {
				markets := []models.Market{enabledMarket, disabledMarket}
				mockSpot.On("ViewMarkets",
					mock.Anything,
					[]models.UserRole{models.UserRoleViewer},
				).Return(markets, nil)
			},
			expectedCode: codes.OK,
			checkResponse: func(t *testing.T, resp *proto.ViewMarketsResponse) {
				assert.NotNil(t, resp)
				assert.Len(t, resp.GetMarkets(), 2)
			},
		},
		{
			name: "успешное получение - несколько ролей",
			request: &proto.ViewMarketsRequest{
				UserRoles: []proto.UserRole{
					proto.UserRole_ROLE_ADMIN,
					proto.UserRole_ROLE_USER,
				},
			},
			setupMocks: func(mockSpot *mocks.SpotInstrument) {
				markets := []models.Market{enabledMarket, disabledMarket, deletedMarket}
				mockSpot.On("ViewMarkets",
					mock.Anything,
					[]models.UserRole{models.UserRoleAdmin, models.UserRoleUser},
				).Return(markets, nil)
			},
			expectedCode: codes.OK,
			checkResponse: func(t *testing.T, resp *proto.ViewMarketsResponse) {
				assert.NotNil(t, resp)
				assert.Len(t, resp.GetMarkets(), 3)
			},
		},
		{
			name: "валидация - user_roles пустой",
			request: &proto.ViewMarketsRequest{
				UserRoles: []proto.UserRole{},
			},
			setupMocks:   func(mockSpot *mocks.SpotInstrument) {},
			expectedCode: codes.InvalidArgument,
			checkResponse: func(t *testing.T, resp *proto.ViewMarketsResponse) {
				assert.Nil(t, resp)
			},
		},
		{
			name: "валидация - user_roles nil",
			request: &proto.ViewMarketsRequest{
				UserRoles: nil,
			},
			setupMocks:   func(mockSpot *mocks.SpotInstrument) {},
			expectedCode: codes.InvalidArgument,
			checkResponse: func(t *testing.T, resp *proto.ViewMarketsResponse) {
				assert.Nil(t, resp)
			},
		},
		{
			name: "валидация - ROLE_UNSPECIFIED в списке",
			request: &proto.ViewMarketsRequest{
				UserRoles: []proto.UserRole{
					proto.UserRole_ROLE_UNSPECIFIED,
				},
			},
			setupMocks:   func(mockSpot *mocks.SpotInstrument) {},
			expectedCode: codes.InvalidArgument,
			checkResponse: func(t *testing.T, resp *proto.ViewMarketsResponse) {
				assert.Nil(t, resp)
			},
		},
		{
			name: "валидация - ROLE_UNSPECIFIED среди валидных ролей",
			request: &proto.ViewMarketsRequest{
				UserRoles: []proto.UserRole{
					proto.UserRole_ROLE_USER,
					proto.UserRole_ROLE_UNSPECIFIED,
				},
			},
			setupMocks:   func(mockSpot *mocks.SpotInstrument) {},
			expectedCode: codes.InvalidArgument,
			checkResponse: func(t *testing.T, resp *proto.ViewMarketsResponse) {
				assert.Nil(t, resp)
			},
		},
		{
			name: "ошибка сервиса - ErrMarketsNotFound",
			request: &proto.ViewMarketsRequest{
				UserRoles: []proto.UserRole{proto.UserRole_ROLE_USER},
			},
			setupMocks: func(mockSpot *mocks.SpotInstrument) {
				mockSpot.On("ViewMarkets",
					mock.Anything,
					[]models.UserRole{models.UserRoleUser},
				).Return(nil, serviceErrors.ErrMarketsNotFound)
			},
			expectedCode: codes.NotFound,
			checkResponse: func(t *testing.T, resp *proto.ViewMarketsResponse) {
				assert.Nil(t, resp)
			},
		},
		{
			name: "ошибка сервиса - внутренняя ошибка",
			request: &proto.ViewMarketsRequest{
				UserRoles: []proto.UserRole{proto.UserRole_ROLE_USER},
			},
			setupMocks: func(mockSpot *mocks.SpotInstrument) {
				mockSpot.On("ViewMarkets",
					mock.Anything,
					[]models.UserRole{models.UserRoleUser},
				).Return(nil, errors.New("internal error"))
			},
			expectedCode: codes.Internal,
			checkResponse: func(t *testing.T, resp *proto.ViewMarketsResponse) {
				assert.Nil(t, resp)
			},
		},
		{
			name: "corner case - большое количество рынков",
			request: &proto.ViewMarketsRequest{
				UserRoles: []proto.UserRole{proto.UserRole_ROLE_ADMIN},
			},
			setupMocks: func(mockSpot *mocks.SpotInstrument) {
				manyMarkets := make([]models.Market, 100)
				for i := 0; i < 100; i++ {
					manyMarkets[i] = models.Market{
						ID:        uuid.New(),
						Name:      gofakeit.Company(),
						Enabled:   true,
						DeletedAt: nil,
					}
				}
				mockSpot.On("ViewMarkets",
					mock.Anything,
					[]models.UserRole{models.UserRoleAdmin},
				).Return(manyMarkets, nil)
			},
			expectedCode: codes.OK,
			checkResponse: func(t *testing.T, resp *proto.ViewMarketsResponse) {
				assert.NotNil(t, resp)
				assert.Len(t, resp.GetMarkets(), 100)

				names := make([]string, len(resp.GetMarkets()))
				for id, market := range resp.GetMarkets() {
					names[id] = market.GetName()
				}
				assert.True(t, isSorted(names), "markets should be sorted")
			},
		},
		{
			name: "проверка маппинга - все поля рынка корректны",
			request: &proto.ViewMarketsRequest{
				UserRoles: []proto.UserRole{proto.UserRole_ROLE_ADMIN},
			},
			setupMocks: func(mockSpot *mocks.SpotInstrument) {
				market := models.Market{
					ID:        uuid.New(),
					Name:      "Test Market",
					Enabled:   true,
					DeletedAt: nil,
				}
				mockSpot.On("ViewMarkets",
					mock.Anything,
					[]models.UserRole{models.UserRoleAdmin},
				).Return([]models.Market{market}, nil)
			},
			expectedCode: codes.OK,
			checkResponse: func(t *testing.T, resp *proto.ViewMarketsResponse) {
				assert.NotNil(t, resp)
				require.Len(t, resp.GetMarkets(), 1)

				protoMarket := resp.GetMarkets()[0]
				assert.NotEmpty(t, protoMarket.GetId())
				assert.Equal(t, "Test Market", protoMarket.GetName())
				assert.True(t, protoMarket.GetEnabled())
				assert.Nil(t, protoMarket.GetDeletedAt())
			},
		},
		{
			name: "проверка маппинга - рынок с DeletedAt",
			request: &proto.ViewMarketsRequest{
				UserRoles: []proto.UserRole{proto.UserRole_ROLE_ADMIN},
			},
			setupMocks: func(mockSpot *mocks.SpotInstrument) {
				deletedTime := time.Now().UTC()
				market := models.Market{
					ID:        uuid.New(),
					Name:      "Deleted Market",
					Enabled:   false,
					DeletedAt: &deletedTime,
				}
				mockSpot.On("ViewMarkets",
					mock.Anything,
					[]models.UserRole{models.UserRoleAdmin},
				).Return([]models.Market{market}, nil)
			},
			expectedCode: codes.OK,
			checkResponse: func(t *testing.T, resp *proto.ViewMarketsResponse) {
				assert.NotNil(t, resp)
				require.Len(t, resp.GetMarkets(), 1)

				protoMarket := resp.GetMarkets()[0]
				assert.NotEmpty(t, protoMarket.GetId())
				assert.Equal(t, "Deleted Market", protoMarket.GetName())
				assert.False(t, protoMarket.GetEnabled())
				assert.NotNil(t, protoMarket.GetDeletedAt())
			},
		},
		{
			name: "проверка сортировки - алфавитный порядок",
			request: &proto.ViewMarketsRequest{
				UserRoles: []proto.UserRole{proto.UserRole_ROLE_ADMIN},
			},
			setupMocks: func(mockSpot *mocks.SpotInstrument) {
				markets := []models.Market{
					{ID: uuid.New(), Name: "ZZZ Market", Enabled: true},
					{ID: uuid.New(), Name: "AAA Market", Enabled: true},
					{ID: uuid.New(), Name: "MMM Market", Enabled: true},
				}
				mockSpot.On("ViewMarkets",
					mock.Anything,
					[]models.UserRole{models.UserRoleAdmin},
				).Return(markets, nil)
			},
			expectedCode: codes.OK,
			checkResponse: func(t *testing.T, resp *proto.ViewMarketsResponse) {
				assert.NotNil(t, resp)
				require.Len(t, resp.GetMarkets(), 3)

				assert.Equal(t, "AAA Market", resp.GetMarkets()[0].GetName())
				assert.Equal(t, "MMM Market", resp.GetMarkets()[1].GetName())
				assert.Equal(t, "ZZZ Market", resp.GetMarkets()[2].GetName())
			},
		},
		{
			name: "corner case - все три роли одновременно",
			request: &proto.ViewMarketsRequest{
				UserRoles: []proto.UserRole{
					proto.UserRole_ROLE_ADMIN,
					proto.UserRole_ROLE_USER,
					proto.UserRole_ROLE_VIEWER,
				},
			},
			setupMocks: func(mockSpot *mocks.SpotInstrument) {
				markets := []models.Market{enabledMarket, disabledMarket, deletedMarket}
				mockSpot.On("ViewMarkets",
					mock.Anything,
					[]models.UserRole{
						models.UserRoleAdmin,
						models.UserRoleUser,
						models.UserRoleViewer,
					},
				).Return(markets, nil)
			},
			expectedCode: codes.OK,
			checkResponse: func(t *testing.T, resp *proto.ViewMarketsResponse) {
				assert.NotNil(t, resp)
				assert.Len(t, resp.GetMarkets(), 3)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {

			mockSpot := new(mocks.SpotInstrument)
			test.setupMocks(mockSpot)

			server := &serverAPI{
				spotInstrument: mockSpot,
			}

			ctx := context.Background()

			response, err := server.ViewMarkets(ctx, test.request)

			if test.expectedCode != codes.OK {
				require.Error(t, err)
				stat, ok := status.FromError(err)
				require.True(t, ok, "error should be a gRPC status error")
				assert.Equal(t, test.expectedCode, stat.Code())
			} else {
				require.NoError(t, err)
			}

			if test.checkResponse != nil {
				test.checkResponse(t, response)
			}

			mockSpot.AssertExpectations(t)
		})
	}
}

func isSorted(names []string) bool {
	for i := 1; i < len(names); i++ {
		if names[i-1] > names[i] {
			return false
		}
	}
	return true
}
