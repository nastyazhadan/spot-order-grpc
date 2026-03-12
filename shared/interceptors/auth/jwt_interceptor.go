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
)

type contextKey string

const UserIDKey contextKey = "user_id"

var skipAuthMethods = map[string]struct{}{
	"/grpc.health.v1.Health/Check": {},
	"/grpc.health.v1.Health/Watch": {},
}

type Claims struct {
	jwt.RegisteredClaims
	UserID string `json:"user_id"`
}

func UnaryServerInterceptor(secret string) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		request any,
		serverInfo *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		if _, ok := skipAuthMethods[serverInfo.FullMethod]; ok {
			return handler(ctx, request)
		}

		md, found := metadata.FromIncomingContext(ctx)
		if !found {
			return nil, status.Error(codes.Unauthenticated, "missing metadata")
		}

		values := md.Get("authorization")
		if len(values) == 0 {
			return nil, status.Error(codes.Unauthenticated, "missing authorization token")
		}

		tokenString := strings.TrimPrefix(values[0], "Bearer ")

		claims := &Claims{}
		token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (any, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, status.Error(codes.Unauthenticated, "unexpected signing method")
			}
			return []byte(secret), nil
		})
		if err != nil || !token.Valid {
			return nil, status.Error(codes.Unauthenticated, "invalid token")
		}

		userID, err := uuid.Parse(claims.UserID)
		if err != nil {
			return nil, status.Error(codes.Unauthenticated, "invalid user_id in token")
		}

		ctx = context.WithValue(ctx, UserIDKey, userID)
		return handler(ctx, request)
	}
}

func UserIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	userID, found := ctx.Value(UserIDKey).(uuid.UUID)
	return userID, found
}
