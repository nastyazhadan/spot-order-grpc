package auth

import (
	"context"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/nastyazhadan/spot-order-grpc/shared/auth/jwt"
	serviceErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/service"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
)

type JWTManager interface {
	GenerateAccessToken(userID uuid.UUID) (string, error)
	GenerateRefreshToken(userID uuid.UUID, jti string) (string, error)
	ParseToken(tokenString string, expectedType jwt.TokenType) (*jwt.Claims, error)
}

type RefreshTokenStore interface {
	Rotate(ctx context.Context, userID uuid.UUID, oldJTI, newJTI string) (bool, error)
	Save(ctx context.Context, userID uuid.UUID, jti string) error
}

type AuthService struct {
	jwtManager JWTManager
	store      RefreshTokenStore
	logger     *zapLogger.Logger
}

func New(jwtManager JWTManager, store RefreshTokenStore, logger *zapLogger.Logger) *AuthService {
	return &AuthService{
		jwtManager: jwtManager,
		store:      store,
		logger:     logger,
	}
}

func (s *AuthService) Issue(
	ctx context.Context,
	userID uuid.UUID,
) (accessToken, refreshToken string, err error) {
	refreshJTI := uuid.NewString()

	accessToken, refreshToken, err = s.generateTokenPair(userID, refreshJTI)
	if err != nil {
		return "", "", err
	}

	if err = s.store.Save(ctx, userID, refreshJTI); err != nil {
		s.logger.Error(ctx, "failed to save refresh token", zap.Error(err))
		return "", "", serviceErrors.ErrSaveTokenFailed
	}

	return accessToken, refreshToken, nil
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
		return uuid.Nil, "", serviceErrors.ErrInvalidSubject
	}

	if claims.ID == "" {
		return uuid.Nil, "", serviceErrors.ErrInvalidJTI
	}

	return userID, claims.ID, nil
}

func (s *AuthService) rotateTokens(
	ctx context.Context,
	userID uuid.UUID,
	oldJTI string,
) (newAccessToken, newRefreshToken string, err error) {
	newJTI := uuid.NewString()

	newAccessToken, newRefreshToken, err = s.generateTokenPair(userID, newJTI)
	if err != nil {
		return "", "", err
	}

	rotated, err := s.store.Rotate(ctx, userID, oldJTI, newJTI)
	if err != nil {
		s.logger.Error(ctx, "failed to rotate refresh token", zap.Error(err))
		return "", "", serviceErrors.ErrSaveTokenFailed
	}
	if !rotated {
		return "", "", serviceErrors.ErrTokenRevoked
	}

	return newAccessToken, newRefreshToken, nil
}

func (s *AuthService) generateTokenPair(
	userID uuid.UUID,
	refreshJTI string,
) (accessToken, refreshToken string, err error) {
	accessToken, err = s.jwtManager.GenerateAccessToken(userID)
	if err != nil {
		return "", "", err
	}

	refreshToken, err = s.jwtManager.GenerateRefreshToken(userID, refreshJTI)
	if err != nil {
		return "", "", err
	}

	return accessToken, refreshToken, nil
}
