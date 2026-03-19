package auth

import (
	"context"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/nastyazhadan/spot-order-grpc/shared/auth/jwt"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
)

type AuthService struct {
	jwtManager jwt.Manager
	store      RefreshTokenStore
	logger     *zapLogger.Logger
}

type RefreshTokenStore interface {
	Save(ctx context.Context, userID uuid.UUID, jti string) error
	Exists(ctx context.Context, userID uuid.UUID, jti string) (bool, error)
	Revoke(ctx context.Context, userID uuid.UUID, jti string) error
}

func NewService(jwtManager jwt.Manager, store RefreshTokenStore, logger *zapLogger.Logger) *AuthService {
	return &AuthService{
		jwtManager: jwtManager,
		store:      store,
		logger:     logger,
	}
}

func (s *AuthService) Refresh(
	ctx context.Context,
	refreshToken string,
) (newAccessToken, newRefreshToken string, err error) {
	userID, oldJTI, err := s.validateRefreshToken(refreshToken)
	if err != nil {
		return "", "", err
	}

	return s.rotateTokens(ctx, userID, oldJTI)
}

func (s *AuthService) validateRefreshToken(refreshToken string) (uuid.UUID, string, error) {
	claims, err := s.jwtManager.ParseToken(refreshToken, jwt.TokenTypeRefresh)
	if err != nil {
		return uuid.Nil, "", err
	}

	userID, err := uuid.Parse(claims.UserID)
	if err != nil {
		return uuid.Nil, "", status.Error(codes.Unauthenticated, "invalid user_id in token")
	}

	if claims.ID == "" {
		return uuid.Nil, "", status.Error(codes.Unauthenticated, "invalid refresh token jti")
	}

	return userID, claims.ID, nil
}

func (s *AuthService) rotateTokens(
	ctx context.Context,
	userID uuid.UUID,
	oldJTI string,
) (newAccessToken, newRefreshToken string, err error) {
	exists, err := s.store.Exists(ctx, userID, oldJTI)
	if err != nil {
		return "", "", status.Error(codes.Internal, "failed to check refresh token")
	}
	if !exists {
		return "", "", status.Error(codes.Unauthenticated, "refresh token revoked or not found")
	}

	newAccess, newRefresh, err := s.issueTokenPair(ctx, userID)
	if err != nil {
		return "", "", err
	}

	if err = s.store.Revoke(ctx, userID, oldJTI); err != nil {
		s.logger.Warn(ctx, "failed to revoke old refresh token", zap.Error(err))
	}

	return newAccess, newRefresh, nil
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
