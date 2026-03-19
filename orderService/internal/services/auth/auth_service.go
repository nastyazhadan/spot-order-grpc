package auth

import (
	"context"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/nastyazhadan/spot-order-grpc/shared/auth/jwt"
)

type AuthService struct {
	jwtManager jwt.Manager
	store      RefreshTokenStore
}

type RefreshTokenStore interface {
	Save(ctx context.Context, userID uuid.UUID, jti string) error
	Exists(ctx context.Context, userID uuid.UUID, jti string) (bool, error)
	Revoke(ctx context.Context, userID uuid.UUID, jti string) error
}

func NewService(jwtManager jwt.Manager, store RefreshTokenStore) *AuthService {
	return &AuthService{
		jwtManager: jwtManager,
		store:      store,
	}
}

func (s *AuthService) Refresh(
	ctx context.Context,
	refreshToken string,
) (newAccessToken, newRefreshToken string, err error) {
	claims, err := s.jwtManager.ParseToken(refreshToken, jwt.TokenTypeRefresh)
	if err != nil {
		return "", "", err
	}

	userID, err := uuid.Parse(claims.UserID)
	if err != nil {
		return "", "", status.Error(codes.Unauthenticated, "invalid user_id in token")
	}

	oldJTI := claims.ID
	if oldJTI == "" {
		return "", "", status.Error(codes.Unauthenticated, "invalid refresh token jti")
	}

	exists, err := s.store.Exists(ctx, userID, oldJTI)
	if err != nil {
		return "", "", status.Error(codes.Internal, "failed to check refresh token")
	}
	if !exists {
		return "", "", status.Error(codes.Unauthenticated, "refresh token revoked or not found")
	}

	if err = s.store.Revoke(ctx, userID, oldJTI); err != nil {
		return "", "", status.Error(codes.Internal, "failed to revoke refresh token")
	}

	return s.issueTokenPair(ctx, userID)
}

func (s *AuthService) issueTokenPair(
	ctx context.Context,
	userID uuid.UUID,
) (accessToken, refreshToken string, err error) {
	jti := uuid.NewString()

	accessToken, err = s.jwtManager.GenerateAccessToken(userID)
	if err != nil {
		return "", "", err
	}

	refreshToken, err = s.jwtManager.GenerateRefreshToken(userID, jti)
	if err != nil {
		return "", "", err
	}

	if err = s.store.Save(ctx, userID, jti); err != nil {
		return "", "", status.Error(codes.Internal, "failed to save refresh token")
	}

	return accessToken, refreshToken, nil
}
