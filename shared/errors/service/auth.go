package service

import "errors"

var (
	ErrMissingMetadata         = errors.New("missing metadata")
	ErrMissingAuthToken        = errors.New("missing authorization token")
	ErrInvalidToken            = errors.New("invalid token")
	ErrTokenExpired            = errors.New("token expired")
	ErrInvalidTokenType        = errors.New("invalid token type")
	ErrMissingTokenSubject     = errors.New("subject not found in token")
	ErrInvalidSubject          = errors.New("invalid subject in token")
	ErrMissingTokenSessionID   = errors.New("session_id not found in token")
	ErrInternalAuthContext     = errors.New("internal auth context error")
	ErrSignAccessTokenFailed   = errors.New("failed to sign access token")
	ErrBuildTokenClaimsFailed  = errors.New("failed to build token claims")
	ErrSessionValidationFailed = errors.New("failed to validate session")

	ErrMissingUserRoles     = errors.New("user_roles not found in token")
	ErrInvalidUserRoles     = errors.New("invalid user_roles in token")
	ErrInvalidUserIDInToken = errors.New("invalid user_id in token")

	ErrInvalidJTI             = errors.New("invalid refresh token jti")
	ErrTokenRevoked           = errors.New("refresh token revoked or not found")
	ErrSaveTokenFailed        = errors.New("failed to save refresh token")
	ErrSignRefreshTokenFailed = errors.New("failed to sign refresh token")
)
