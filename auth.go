package main

import (
	"context"
	"net/http"
	"os"

	"github.com/golang-jwt/jwt/v5"
)

var jwtKey = []byte(os.Getenv("JWT_SECRET"))

type Claims struct {
	UserID int `json:"user_id"`
	jwt.RegisteredClaims
}

func AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("token")
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		tokenStr := cookie.Value
		claims := &Claims{}

		token, err := jwt.ParseWithClaims(tokenStr, claims, func(token *jwt.Token) (interface{}, error) {
			return jwtKey, nil
		})

		if err != nil || !token.Valid {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), "user_id", claims.UserID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func VerifyTurnstile(token string) bool {
	// For local dev/testing, if TURNSTILE_SECRET is empty, bypass
	secret := os.Getenv("TURNSTILE_SECRET")
	if secret == "" {
		return true
	}
	// TODO: Implement actual verify check to Cloudflare API
	return true
}

func SendOTP(email, code string) error {
	// Resend integration placeholder
	// resendKey := os.Getenv("RESEND_KEY")
	// if resendKey == "" { ... }
	println("OTP for", email, "is", code)
	return nil
}
