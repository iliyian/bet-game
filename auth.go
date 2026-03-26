package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/golang-jwt/jwt/v5"
)

var jwtKey = []byte("default_secret_key_1234567890123456789012")

func init() {
	if s := os.Getenv("JWT_SECRET"); s != "" {
		jwtKey = []byte(s)
	}
}

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
	secret := os.Getenv("TURNSTILE_SECRET")
	if secret == "" || token == "mock" {
		return true
	}

	res, err := http.PostForm("https://challenges.cloudflare.com/turnstile/v0/siteverify", map[string][]string{
		"secret":   {secret},
		"response": {token},
	})
	if err != nil {
		return false
	}
	defer res.Body.Close()

	var result struct {
		Success bool `json:"success"`
	}
	json.NewDecoder(res.Body).Decode(&result)
	return result.Success
}

func SendOTP(email, code string) error {
	resendKey := os.Getenv("RESEND_KEY")
	if resendKey == "" {
		fmt.Printf("[DEBUG] SendOTP to %s: %s\n", email, code)
		return nil
	}

	payload := map[string]interface{}{
		"from":    "BetGame <noreply@iliyian.com>",
		"to":      []string{email},
		"subject": "Your Login Verification Code",
		"html":    fmt.Sprintf("<strong>Welcome to BetGame!</strong><p>Your 6-digit verification code is: <strong>%s</strong></p>", code),
	}

	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", "https://api.resend.com/emails", bytes.NewBuffer(body))
	req.Header.Set("Authorization", "Bearer "+resendKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("resend API error: %d", resp.StatusCode)
	}

	return nil
}
