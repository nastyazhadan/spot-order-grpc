package auth

import (
	"context"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	authjwt "github.com/nastyazhadan/spot-order-grpc/shared/auth/jwt"
	authErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/service"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
	"github.com/nastyazhadan/spot-order-grpc/shared/models"
)

type JWTManager interface {
	GenerateAccessToken(userID uuid.UUID, roles []models.UserRole, sessionID string) (string, error)
	GenerateRefreshToken(userID uuid.UUID, roles []models.UserRole, jti, sessionID string) (string, error)
	ParseToken(tokenString string, expectedType authjwt.TokenType) (*authjwt.Claims, error)
}

type RefreshTokenStore interface {
	Rotate(ctx context.Context, userID uuid.UUID, oldJTI, oldSessionID, newJTI, newSessionID string) (bool, error)
	Replace(ctx context.Context, userID uuid.UUID, newJTI, newSessionID string) error
}

type SessionStore interface {
	IsSessionActive(ctx context.Context, userID uuid.UUID, sessionID string) (bool, error)
}

type AuthService struct {
	jwtManager   JWTManager
	refreshStore RefreshTokenStore
	sessionStore SessionStore
	timeout      time.Duration
	logger       *zapLogger.Logger
}

func New(
	jwtManager JWTManager,
	refreshStore RefreshTokenStore,
	sessionStore SessionStore,
	timeout time.Duration,
	logger *zapLogger.Logger,
) *AuthService {
	return &AuthService{
		jwtManager:   jwtManager,
		refreshStore: refreshStore,
		sessionStore: sessionStore,
		timeout:      timeout,
		logger:       logger,
	}
}

func (s *AuthService) Refresh(
	ctx context.Context,
	refreshToken string,
) (newAccessToken, newRefreshToken string, err error) {
	ctx, cancel := contextWithTimeout(ctx, s.timeout)
	defer cancel()

	userID, roles, oldJTI, oldSessionID, err := s.validateRefreshToken(ctx, refreshToken)
	if err != nil {
		return "", "", err
	}

	return s.rotateTokens(ctx, userID, roles, oldJTI, oldSessionID)
}

func (s *AuthService) validateRefreshToken(
	ctx context.Context,
	refreshToken string,
) (uuid.UUID, []models.UserRole, string, string, error) {
	claims, err := s.jwtManager.ParseToken(refreshToken, authjwt.TokenTypeRefresh)
	if err != nil {
		return uuid.Nil, nil, "", "", err
	}

	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return uuid.Nil, nil, "", "", authErrors.ErrInvalidSubject
	}

	if claims.ID == "" {
		return uuid.Nil, nil, "", "", authErrors.ErrInvalidJTI
	}

	roles, err := authjwt.ParseUserRolesClaims(claims.UserRoles)
	if err != nil {
		return uuid.Nil, nil, "", "", err
	}

	active, err := s.sessionStore.IsSessionActive(ctx, userID, claims.SessionID)
	if err != nil {
		s.logger.Error(ctx, "failed to validate refresh token session", zap.Error(err))
		return uuid.Nil, nil, "", "", authErrors.ErrSessionValidationFailed
	}
	if !active {
		return uuid.Nil, nil, "", "", authErrors.ErrTokenRevoked
	}

	return userID, roles, claims.ID, claims.SessionID, nil
}

func (s *AuthService) rotateTokens(
	ctx context.Context,
	userID uuid.UUID,
	roles []models.UserRole,
	oldJTI, oldSessionID string,
) (newAccessToken, newRefreshToken string, err error) {
	newJTI := uuid.NewString()
	newSessionID := oldSessionID

	newAccessToken, newRefreshToken, err = s.generateTokenPair(userID, roles, newJTI, newSessionID)
	if err != nil {
		return "", "", err
	}

	rotated, err := s.refreshStore.Rotate(ctx, userID, oldJTI, oldSessionID, newJTI, newSessionID)
	if err != nil {
		s.logger.Error(ctx, "failed to rotate refresh token", zap.Error(err))
		return "", "", authErrors.ErrSaveTokenFailed
	}
	if !rotated {
		return "", "", authErrors.ErrTokenRevoked
	}

	return newAccessToken, newRefreshToken, nil
}

func (s *AuthService) generateTokenPair(
	userID uuid.UUID,
	roles []models.UserRole,
	refreshJTI, sessionID string,
) (accessToken, refreshToken string, err error) {
	accessToken, err = s.jwtManager.GenerateAccessToken(userID, roles, sessionID)
	if err != nil {
		return "", "", err
	}

	refreshToken, err = s.jwtManager.GenerateRefreshToken(userID, roles, refreshJTI, sessionID)
	if err != nil {
		return "", "", err
	}

	return accessToken, refreshToken, nil
}

func contextWithTimeout(
	ctx context.Context,
	timeout time.Duration,
) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}

	if timeout <= 0 {
		return ctx, func() {}
	}

	if deadline, ok := ctx.Deadline(); ok {
		if time.Until(deadline) <= timeout {
			return ctx, func() {}
		}
	}

	return context.WithTimeout(ctx, timeout)
}
