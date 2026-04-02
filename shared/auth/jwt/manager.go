package jwt

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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

func (m *Manager) GenerateAccessToken(userID uuid.UUID, sessionID string) (string, error) {
	now := time.Now()

	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(m.accessTTL)),
		},
		TokenType: TokenTypeAccess,
		SessionID: sessionID,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	signed, err := token.SignedString(m.secret)
	if err != nil {
		return "", status.Error(codes.Internal, "failed to sign access token")
	}

	return signed, nil
}

func (m *Manager) GenerateRefreshToken(userID uuid.UUID, jti, sessionID string) (string, error) {
	now := time.Now()

	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        jti,
			Subject:   userID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(m.refreshTTL)),
		},
		TokenType: TokenTypeRefresh,
		SessionID: sessionID,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	signed, err := token.SignedString(m.secret)
	if err != nil {
		return "", status.Error(codes.Internal, "failed to sign refresh token")
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
		return nil, status.Error(codes.Unauthenticated, "token expired")
	}

	if err != nil || token == nil || !token.Valid {
		return nil, status.Error(codes.Unauthenticated, "invalid token")
	}

	if claims.TokenType != expectedType {
		return nil, status.Error(codes.Unauthenticated, "invalid token type")
	}

	if claims.Subject == "" {
		return nil, status.Error(codes.Unauthenticated, "subject not found in token")
	}

	if claims.SessionID == "" {
		return nil, status.Error(codes.Unauthenticated, "session_id not found in token")
	}

	return claims, nil
}
