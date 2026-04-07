package auth

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	authjwt "github.com/nastyazhadan/spot-order-grpc/shared/auth/jwt"
	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	authErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/service"
	"github.com/nastyazhadan/spot-order-grpc/shared/requestctx"
)

const (
	authorizationHeader = "authorization"
	bearerPrefix        = "bearer "
)

type TokenParser interface {
	ParseToken(tokenString string, expectedType authjwt.TokenType) (*authjwt.Claims, error)
}

func UnaryServerInterceptor(
	jwtManager TokenParser,
	cfg config.AuthVerifierConfig,
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
			return nil, authErrors.ErrInvalidUserIDInToken
		}

		ctx, ok := requestctx.ContextWithUserID(ctx, userID)
		if !ok {
			return nil, authErrors.ErrInternalAuthContext
		}
		ctx, ok = requestctx.ContextWithUserRoles(ctx, userRoles)
		if !ok {
			return nil, authErrors.ErrInternalAuthContext
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
		return "", authErrors.ErrMissingMetadata
	}

	values := md.Get(authorizationHeader)
	if len(values) == 0 {
		return "", authErrors.ErrMissingAuthToken
	}

	authHeader := strings.TrimSpace(values[0])
	lower := strings.ToLower(authHeader)
	if !strings.HasPrefix(lower, bearerPrefix) {
		return "", authErrors.ErrMissingAuthToken
	}

	tokenString := strings.TrimSpace(authHeader[len(bearerPrefix):])
	if tokenString == "" {
		return "", authErrors.ErrMissingAuthToken
	}

	return tokenString, nil
}
