package jwt

import (
	"github.com/golang-jwt/jwt/v5"
)

type TokenType string

const (
	TokenTypeAccess  TokenType = "access"
	TokenTypeRefresh TokenType = "refresh"
)

type Claims struct {
	jwt.RegisteredClaims
	TokenType TokenType `json:"token_type"`
	SessionID string    `json:"session_id"`
}
