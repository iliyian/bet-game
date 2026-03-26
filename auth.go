package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var jwtKey []byte

// Use unexported type for context key to avoid collisions.
type contextKey string

const ctxUserID contextKey = "user_id"

func initJWT() {
	s := os.Getenv("JWT_SECRET")
	if s == "" {
		panic("JWT_SECRET 环境变量未设置")
	}
	jwtKey = []byte(s)
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
			// Explicitly validate signing algorithm to prevent algorithm confusion attacks.
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return jwtKey, nil
		})

		if err != nil || !token.Valid {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), ctxUserID, claims.UserID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GenerateOTP generates a cryptographically secure 6-digit OTP.
func GenerateOTP() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

// OTP rate limiter: track failed verification attempts per email.
var (
	otpAttempts   = make(map[string]*otpAttemptInfo)
	otpAttemptsMu sync.Mutex
)

type otpAttemptInfo struct {
	Count     int
	FirstTry  time.Time
}

const (
	otpMaxAttempts  = 5
	otpWindowPeriod = 5 * time.Minute
)

// CheckOTPRateLimit returns true if the email is rate-limited.
func CheckOTPRateLimit(email string) bool {
	otpAttemptsMu.Lock()
	defer otpAttemptsMu.Unlock()

	info, exists := otpAttempts[email]
	if !exists {
		return false
	}
	if time.Since(info.FirstTry) > otpWindowPeriod {
		delete(otpAttempts, email)
		return false
	}
	return info.Count >= otpMaxAttempts
}

// RecordOTPFailure records a failed OTP verification attempt.
func RecordOTPFailure(email string) {
	otpAttemptsMu.Lock()
	defer otpAttemptsMu.Unlock()

	info, exists := otpAttempts[email]
	if !exists || time.Since(info.FirstTry) > otpWindowPeriod {
		otpAttempts[email] = &otpAttemptInfo{Count: 1, FirstTry: time.Now()}
		return
	}
	info.Count++
}

// ClearOTPAttempts clears rate limit state after successful verification.
func ClearOTPAttempts(email string) {
	otpAttemptsMu.Lock()
	defer otpAttemptsMu.Unlock()
	delete(otpAttempts, email)
}

func VerifyTurnstile(token string) bool {
	secret := os.Getenv("TURNSTILE_SECRET")
	if secret == "" {
		panic("TURNSTILE_SECRET 环境变量未设置")
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
		return fmt.Errorf("RESEND_KEY 环境变量未设置")
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
