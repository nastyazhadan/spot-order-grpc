package jwt

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	authErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/service"
	"github.com/nastyazhadan/spot-order-grpc/shared/models"
)

type Manager struct {
	secret     []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
}

func NewManager(secret string, accessTTL, refreshTTL time.Duration) *Manager {
	return &Manager{
		secret:     []byte(secret),
		accessTTL:  accessTTL,
		refreshTTL: refreshTTL,
	}
}

func (m *Manager) GenerateAccessToken(userID uuid.UUID, roles []models.UserRole, sessionID string) (string, error) {
	now := time.Now()

	userRoles, err := UserRolesToClaims(roles)
	if err != nil {
		return "", err
	}

	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(m.accessTTL)),
		},
		TokenType: TokenTypeAccess,
		SessionID: sessionID,
		UserRoles: userRoles,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	signed, err := token.SignedString(m.secret)
	if err != nil {
		return "", authErrors.ErrSignAccessTokenFailed
	}

	return signed, nil
}

func (m *Manager) GenerateRefreshToken(userID uuid.UUID, roles []models.UserRole, jti, sessionID string) (string, error) {
	now := time.Now()

	userRoles, err := UserRolesToClaims(roles)
	if err != nil {
		return "", err
	}

	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        jti,
			Subject:   userID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(m.refreshTTL)),
		},
		TokenType: TokenTypeRefresh,
		SessionID: sessionID,
		UserRoles: userRoles,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	signed, err := token.SignedString(m.secret)
	if err != nil {
		return "", authErrors.ErrSignRefreshTokenFailed
	}

	return signed, nil
}

func (m *Manager) ParseToken(tokenString string, expectedType TokenType) (*Claims, error) {
	claims := &Claims{}

	token, err := jwt.ParseWithClaims(
		tokenString,
		claims,
		func(token *jwt.Token) (any, error) {
			return m.secret, nil
		},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
	)

	if errors.Is(err, jwt.ErrTokenExpired) {
		return nil, authErrors.ErrTokenExpired
	}

	if err != nil || token == nil || !token.Valid {
		return nil, authErrors.ErrInvalidToken
	}

	if claims.TokenType != expectedType {
		return nil, authErrors.ErrInvalidTokenType
	}

	if claims.Subject == "" {
		return nil, authErrors.ErrMissingTokenSubject
	}

	if claims.SessionID == "" {
		return nil, authErrors.ErrMissingTokenSessionID
	}

	return claims, nil
}
