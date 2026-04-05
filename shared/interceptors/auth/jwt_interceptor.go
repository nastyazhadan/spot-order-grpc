package auth

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	authjwt "github.com/nastyazhadan/spot-order-grpc/shared/auth/jwt"
	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	"github.com/nastyazhadan/spot-order-grpc/shared/requestctx"
)

const (
	authorizationHeader = "authorization"
	bearerPrefix        = "bearer "
)

type TokenParser interface {
	ParseToken(tokenString string, expectedType authjwt.TokenType) (*authjwt.Claims, error)
}

type SessionStore interface {
	IsSessionActive(ctx context.Context, userID uuid.UUID, sessionID string) (bool, error)
}

func UnaryServerInterceptor(
	jwtManager TokenParser,
	sessionStore SessionStore,
	cfg config.AuthConfig,
) grpc.UnaryServerInterceptor {
	skipMethods := makeSkipMethods(cfg.SkipMethods)

	return func(
		ctx context.Context,
		request any,
		serverInfo *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		if shouldSkip(serverInfo.FullMethod, skipMethods) {
			return handler(ctx, request)
		}

		tokenString, err := bearerTokenFromContext(ctx)
		if err != nil {
			return nil, err
		}

		claims, err := jwtManager.ParseToken(tokenString, authjwt.TokenTypeAccess)
		if err != nil {
			return nil, err
		}

		userRoles, err := authjwt.ParseUserRolesClaims(claims.UserRoles)
		if err != nil {
			return nil, err
		}

		userID, err := uuid.Parse(claims.Subject)
		if err != nil {
			return nil, status.Error(codes.Unauthenticated, "invalid user_id in token")
		}

		if claims.SessionID == "" {
			return nil, status.Error(codes.Unauthenticated, "invalid token")
		}

		active, err := sessionStore.IsSessionActive(ctx, userID, claims.SessionID)
		if err != nil {
			return nil, status.Error(codes.Internal, "internal auth error")
		}
		if !active {
			return nil, status.Error(codes.Unauthenticated, "invalid token")
		}

		ctx, ok := requestctx.ContextWithUserID(ctx, userID)
		if !ok {
			return nil, status.Error(codes.Internal, "internal auth context error")
		}
		ctx, ok = requestctx.ContextWithUserRoles(ctx, userRoles)
		if !ok {
			return nil, status.Error(codes.Internal, "internal auth context error")
		}

		return handler(ctx, request)
	}
}

func makeSkipMethods(methods []string) map[string]struct{} {
	skipMethods := make(map[string]struct{}, len(methods))
	for _, method := range methods {
		method = strings.TrimSpace(method)
		if method == "" {
			continue
		}
		skipMethods[method] = struct{}{}
	}

	return skipMethods
}

func shouldSkip(fullMethod string, skipMethods map[string]struct{}) bool {
	_, ok := skipMethods[fullMethod]

	return ok
}

func bearerTokenFromContext(ctx context.Context) (string, error) {
	md, found := metadata.FromIncomingContext(ctx)
	if !found {
		return "", status.Error(codes.Unauthenticated, "missing metadata")
	}

	values := md.Get(authorizationHeader)
	if len(values) == 0 {
		return "", status.Error(codes.Unauthenticated, "missing authorization token")
	}

	authHeader := strings.TrimSpace(values[0])
	lower := strings.ToLower(authHeader)
	if !strings.HasPrefix(lower, bearerPrefix) {
		return "", status.Error(codes.Unauthenticated, "missing authorization token")
	}

	tokenString := strings.TrimSpace(authHeader[len(bearerPrefix):])
	if tokenString == "" {
		return "", status.Error(codes.Unauthenticated, "missing authorization token")
	}

	return tokenString, nil
}
