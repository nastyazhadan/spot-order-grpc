package main

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/joho/godotenv"

	authjwt "github.com/nastyazhadan/spot-order-grpc/shared/auth/jwt"
)

const (
	defaultUserID          = "550e8400-e29b-41d4-a716-446655440003"
	defaultAccessTokenTTL  = 5 * time.Minute
	defaultRefreshTokenTTL = 10 * time.Minute
)

func main() {
	for _, path := range []string{".env", "../.env", "../../.env"} {
		if err := godotenv.Load(path); err == nil {
			break
		}
	}

	secret := strings.TrimSpace(os.Getenv("JWT_SECRET"))
	if secret == "" {
		log.Fatal("JWT_SECRET environment variable must be set")
	}

	userID, err := uuid.Parse(defaultUserID)
	if err != nil {
		log.Fatalf("invalid default user id: %v", err)
	}

	jwtManager := authjwt.NewManager(
		secret,
		defaultAccessTokenTTL,
		defaultRefreshTokenTTL,
	)

	accessToken, err := jwtManager.GenerateAccessToken(userID)
	if err != nil {
		log.Fatalf("failed to generate access token: %v", err)
	}

	fmt.Println(accessToken)
}
