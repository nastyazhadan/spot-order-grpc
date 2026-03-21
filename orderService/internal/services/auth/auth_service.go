package auth

import (
	"context"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/nastyazhadan/spot-order-grpc/shared/auth/jwt"
	errors "github.com/nastyazhadan/spot-order-grpc/shared/errors/service"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
)

type AuthService struct {
	jwtManager jwt.Manager
	store      RefreshTokenStore
	logger     *zapLogger.Logger
}

type RefreshTokenStore interface {
	Save(ctx context.Context, userID uuid.UUID, jti string) error
	RevokeIfExists(ctx context.Context, userID uuid.UUID, jti string) (bool, error)
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

	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return uuid.Nil, "", errors.ErrInvalidSubject
	}

	if claims.ID == "" {
		return uuid.Nil, "", errors.ErrInvalidJTI
	}

	return userID, claims.ID, nil
}

func (s *AuthService) rotateTokens(
	ctx context.Context,
	userID uuid.UUID,
	oldJTI string,
) (newAccessToken, newRefreshToken string, err error) {
	revoked, err := s.store.RevokeIfExists(ctx, userID, oldJTI)
	if err != nil {
		return "", "", errors.ErrRevokeTokenFailed
	}
	if !revoked {
		return "", "", errors.ErrTokenRevoked
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
		s.logger.Error(ctx, "failed to save refresh token", zap.Error(err))
		return "", "", errors.ErrSaveTokenFailed
	}

	return accessToken, refreshToken, nil
}
