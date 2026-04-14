//go:build integration

package suite

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	pgContainer "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	proto "github.com/nastyazhadan/spot-order-grpc/protos/gen/go/spot/v1"
	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/cache"
	migrate "github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/db/migrator"
	grpcErrors "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/errors"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/recovery"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/validate"
	sharedModels "github.com/nastyazhadan/spot-order-grpc/shared/models"
	"github.com/nastyazhadan/spot-order-grpc/shared/requestctx"
	grpcSpot "github.com/nastyazhadan/spot-order-grpc/spotService/internal/grpc/spot"
	repoPostgres "github.com/nastyazhadan/spot-order-grpc/spotService/internal/infrastructure/postgres/spot"
	repoRedis "github.com/nastyazhadan/spot-order-grpc/spotService/internal/infrastructure/redis"
	svcSpot "github.com/nastyazhadan/spot-order-grpc/spotService/internal/services/spot"
	"github.com/nastyazhadan/spot-order-grpc/spotService/migrations"
)

const (
	dbUser     = "test_user"
	dbPassword = "test_password"
	dbName     = "spot_test_db"

	longTimeout    = 2 * time.Minute
	startupTimeout = 30 * time.Second

	testRolesMetadataKey = "x-test-roles"

	testServiceName    = "spot-integration-test"
	testCacheTTL       = 5 * time.Minute
	testServiceTimeout = 10 * time.Second
	testDefaultLimit   = uint64(20)
	testMaxLimit       = uint64(100)
	testCacheLimit     = uint64(50)
)

type Suite struct {
	Test       *testing.T
	SpotClient proto.SpotInstrumentServiceClient
	Pool       *pgxpool.Pool
	Redis      *redis.Client
}

func New(test *testing.T) (context.Context, *Suite) {
	test.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), longTimeout)
	test.Cleanup(cancel)

	logger := zapLogger.NewNop()

	container, err := pgContainer.Run(ctx,
		"postgres:17.0-alpine3.20",
		pgContainer.WithDatabase(dbName),
		pgContainer.WithUsername(dbUser),
		pgContainer.WithPassword(dbPassword),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(startupTimeout),
		),
	)
	if err != nil {
		test.Fatalf("failed to start postgres container: %v", err)
	}
	test.Cleanup(func() {
		if err = container.Terminate(context.Background()); err != nil {
			test.Logf("failed to terminate postgres container: %v", err)
		}
	})

	connString, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		test.Fatalf("failed to get connection string: %v", err)
	}

	pool, err := pgxpool.New(ctx, connString)
	if err != nil {
		test.Fatalf("failed to create pgxpool: %v", err)
	}
	if err = pool.Ping(ctx); err != nil {
		test.Fatalf("failed to ping postgres: %v", err)
	}
	test.Cleanup(pool.Close)

	sqlDB := stdlib.OpenDBFromPool(pool)
	test.Cleanup(func() {
		_ = sqlDB.Close()
	})

	if err = migrate.NewMigrator(sqlDB, migrations.Migrations).Up(ctx); err != nil {
		test.Fatalf("failed to run migrations: %v", err)
	}

	redisC, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		Started: true,
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "redis:7-alpine",
			ExposedPorts: []string{"6379/tcp"},
			WaitingFor: wait.ForListeningPort("6379/tcp").
				WithStartupTimeout(startupTimeout),
		},
	})
	if err != nil {
		test.Fatalf("failed to start redis container: %v", err)
	}
	test.Cleanup(func() {
		if err = redisC.Terminate(context.Background()); err != nil {
			test.Logf("failed to terminate redis container: %v", err)
		}
	})

	redisHost, err := redisC.Host(ctx)
	if err != nil {
		test.Fatalf("failed to get redis host: %v", err)
	}
	redisPort, err := redisC.MappedPort(ctx, "6379/tcp")
	if err != nil {
		test.Fatalf("failed to get redis port: %v", err)
	}

	redisClient := redis.NewClient(&redis.Options{
		Addr: fmt.Sprintf("%s:%s", redisHost, redisPort.Port()),
	})
	if err = redisClient.Ping(ctx).Err(); err != nil {
		test.Fatalf("failed to ping redis: %v", err)
	}
	test.Cleanup(func() {
		_ = redisClient.Close()
	})

	cacheStore := cache.New(redisClient)

	spotCfg := config.SpotConfig{
		Service: config.ServiceConfig{Name: testServiceName},
	}

	marketRepo := repoPostgres.NewMarketStore(pool, spotCfg)
	cacheRepo := repoRedis.NewMarketCacheRepository(cacheStore, testServiceName)
	biIDCacheRepo := repoRedis.NewMarketByIDCacheRepository(cacheStore, testServiceName)

	marketSvc := svcSpot.NewMarketViewer(
		marketRepo,
		cacheRepo,
		biIDCacheRepo,
		testCacheTTL,
		testServiceTimeout,
		testDefaultLimit,
		testMaxLimit,
		testCacheLimit,
		testServiceName,
		logger,
	)

	validator, err := validate.UnaryServerInterceptor()
	if err != nil {
		test.Fatalf("validate.UnaryServerInterceptor: %v", err)
	}

	grpcServer := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			recovery.UnaryServerInterceptor(logger),
			testRolesInjectorInterceptor,
			validator,
			grpcErrors.UnaryServerInterceptor(logger),
		),
	)
	grpcSpot.Register(grpcServer, marketSvc)

	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		test.Fatalf("failed to create listener: %v", err)
	}

	go func() {
		_ = grpcServer.Serve(listener)
	}()
	test.Cleanup(grpcServer.GracefulStop)

	address := fmt.Sprintf("localhost:%d", listener.Addr().(*net.TCPAddr).Port)
	connection, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		test.Fatalf("failed to connect to gRPC server: %v", err)
	}
	test.Cleanup(func() {
		_ = connection.Close()
	})

	return ctx, &Suite{
		Test:       test,
		SpotClient: proto.NewSpotInstrumentServiceClient(connection),
		Pool:       pool,
		Redis:      redisClient,
	}
}

func (s *Suite) CtxWithRole(ctx context.Context, roles ...sharedModels.UserRole) context.Context {
	strRoles := make([]string, len(roles))
	for i, r := range roles {
		strRoles[i] = r.String()
	}
	return metadata.NewOutgoingContext(ctx, metadata.Pairs(testRolesMetadataKey, strings.Join(strRoles, ",")))
}

func (s *Suite) ClearMarkets(ctx context.Context) {
	s.Test.Helper()

	if _, err := s.Pool.Exec(ctx, "DELETE FROM market_store"); err != nil {
		s.Test.Fatalf("failed to clear market_store: %v", err)
	}
}

func (s *Suite) FlushRedis(ctx context.Context) {
	s.Test.Helper()

	if err := s.Redis.FlushDB(ctx).Err(); err != nil {
		s.Test.Fatalf("failed to flush redis: %v", err)
	}
}

func (s *Suite) InsertMarket(ctx context.Context, id, name string, enabled bool, deletedAt *time.Time) {
	s.Test.Helper()

	_, err := s.Pool.Exec(ctx,
		`INSERT INTO market_store (id, name, enabled, deleted_at) VALUES ($1, $2, $3, $4)`,
		id, name, enabled, deletedAt,
	)
	if err != nil {
		s.Test.Fatalf("failed to insert market %q: %v", name, err)
	}
}

func (s *Suite) DeleteMarketFromDB(ctx context.Context, id string) {
	s.Test.Helper()

	if _, err := s.Pool.Exec(ctx, "DELETE FROM market_store WHERE id = $1", id); err != nil {
		s.Test.Fatalf("failed to delete market %s from DB: %v", id, err)
	}
}

func testRolesInjectorInterceptor(
	ctx context.Context,
	request any,
	_ *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (any, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return handler(ctx, request)
	}

	values := md.Get(testRolesMetadataKey)
	if len(values) == 0 {
		return handler(ctx, request)
	}

	var roles []sharedModels.UserRole
	for _, part := range strings.Split(values[0], ",") {
		if r, ok := sharedModels.ParseUserRole(strings.TrimSpace(part)); ok {
			roles = append(roles, r)
		}
	}

	if len(roles) > 0 {
		ctx, _ = requestctx.ContextWithUserRoles(ctx, roles)
	}

	return handler(ctx, request)
}

func MarketIDs(response *proto.ViewMarketsResponse) []string {
	if response == nil {
		return nil
	}
	ids := make([]string, 0, len(response.GetMarkets()))
	for _, m := range response.GetMarkets() {
		ids = append(ids, m.GetId())
	}
	return ids
}
