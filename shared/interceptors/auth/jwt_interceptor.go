package auth

import (
	"context"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	"github.com/nastyazhadan/spot-order-grpc/shared/requestctx"
)

const (
	authorizationHeader = "authorization"
	bearerPrefix        = "bearer "
)

type Claims struct {
	jwt.RegisteredClaims
	UserID string `json:"user_id"`
}

func UnaryServerInterceptor(secret string, cfg config.AuthConfig) grpc.UnaryServerInterceptor {
	skipMethods := make(map[string]struct{}, len(cfg.SkipMethods))
	for _, method := range cfg.SkipMethods {
		method = strings.TrimSpace(method)
		if method == "" {
			continue
		}
		skipMethods[method] = struct{}{}
	}

	return func(
		ctx context.Context,
		request any,
		serverInfo *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		if _, ok := skipMethods[serverInfo.FullMethod]; ok {
			return handler(ctx, request)
		}

		md, found := metadata.FromIncomingContext(ctx)
		if !found {
			return nil, status.Error(codes.Unauthenticated, "missing metadata")
		}

		values := md.Get(authorizationHeader)
		if len(values) == 0 || !strings.HasPrefix(strings.ToLower(values[0]), bearerPrefix) {
			return nil, status.Error(codes.Unauthenticated, "missing authorization token")
		}

		userID, err := parseUserIDFromToken(secret, values[0])
		if err != nil {
			return nil, err
		}

		ctx = requestctx.ContextWithUserID(ctx, userID)
		return handler(ctx, request)
	}
}

func parseUserIDFromToken(secret, authHeader string) (uuid.UUID, error) {
	tokenString := strings.TrimSpace(authHeader[len(bearerPrefix):])

	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, status.Error(codes.Unauthenticated, "unexpected signing method")
		}
		return []byte(secret), nil
	})
	if err != nil || !token.Valid {
		return uuid.Nil, status.Error(codes.Unauthenticated, "invalid token")
	}

	userID, err := uuid.Parse(claims.UserID)
	if err != nil {
		return uuid.Nil, status.Error(codes.Unauthenticated, "invalid user_id in token")
	}

	return userID, nil
}
