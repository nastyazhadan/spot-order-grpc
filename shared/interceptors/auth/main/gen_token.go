package main

import (
	"fmt"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/joho/godotenv"

	"github.com/nastyazhadan/spot-order-grpc/shared/config"
)

func main() {
	for _, path := range []string{".env", "../.env", "../../.env"} {
		if err := godotenv.Load(path); err == nil {
			break
		}
	}

	var cfg config.OrderConfig
	cfg.JWTSecret = os.Getenv("JWT_SECRET")

	claims := jwt.MapClaims{
		"user_id": "550e8400-e29b-41d4-a716-446655440000",
		"exp":     time.Now().Add(24 * time.Hour).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(cfg.JWTSecret))
	if err != nil {
		panic(err)
	}

	fmt.Println(signed)
}
