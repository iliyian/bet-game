package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func RegisterHandler(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email          string `json:"email"`
		TurnstileToken string `json:"turnstile_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	if !VerifyTurnstile(input.TurnstileToken) {
		http.Error(w, "安全验证未通过", http.StatusForbidden)
		return
	}

	code := fmt.Sprintf("%06d", rand.Intn(1000000))
	expires := time.Now().Add(5 * time.Minute)

	_, err := db.Exec("INSERT OR REPLACE INTO otp_codes (email, code, expires_at) VALUES (?, ?, ?)", input.Email, code, expires)
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	SendOTP(input.Email, code)
	w.WriteHeader(http.StatusOK)
}

func VerifyHandler(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email string `json:"email"`
		Code  string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	var storedCode string
	var expiresAt time.Time
	err := db.QueryRow("SELECT code, expires_at FROM otp_codes WHERE email = ?", input.Email).Scan(&storedCode, &expiresAt)
	if err != nil || storedCode != input.Code || time.Now().After(expiresAt) {
		http.Error(w, "验证码无效或已过期", http.StatusUnauthorized)
		return
	}

	// Create user if not exists
	_, err = db.Exec("INSERT OR IGNORE INTO users (email) VALUES (?)", input.Email)
	var userID int
	db.QueryRow("SELECT id FROM users WHERE email = ?", input.Email).Scan(&userID)

	// Set JWT Cookie
	expirationTime := time.Now().Add(24 * time.Hour)
	claims := &Claims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expirationTime),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, _ := token.SignedString(jwtKey)

	http.SetCookie(w, &http.Cookie{
		Name:    "token",
		Value:   tokenString,
		Expires: expirationTime,
		Path:    "/",
	})

	w.WriteHeader(http.StatusOK)
}

func GetMeHandler(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value("user_id").(int)
	var balance int
	var email, username sql.NullString
	db.QueryRow("SELECT email, username, balance FROM users WHERE id = ?", userID).Scan(&email, &username, &balance)

	json.NewEncoder(w).Encode(map[string]interface{}{
		"email":    email.String,
		"username": username.String,
		"balance":  balance,
	})
}

func UpdateUsernameHandler(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value("user_id").(int)
	var input struct {
		Username string `json:"username"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	if len(input.Username) < 3 || len(input.Username) > 20 {
		http.Error(w, "用户名长度必须在3-20之间", http.StatusBadRequest)
		return
	}

	_, err := db.Exec("UPDATE users SET username = ? WHERE id = ?", input.Username, userID)
	if err != nil {
		http.Error(w, "用户名已被占用", http.StatusConflict)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func PlaceBetHandler(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value("user_id").(int)
	var input struct {
		Amount int `json:"amount"`
	}
	json.NewDecoder(r.Body).Decode(&input)

	if input.Amount <= 0 {
		http.Error(w, "请输入有效的投注金额", http.StatusBadRequest)
		return
	}

	tx, _ := db.Begin()
	defer tx.Rollback()

	var balance int
	tx.QueryRow("SELECT balance FROM users WHERE id = ?", userID).Scan(&balance)
	if balance < input.Amount {
		http.Error(w, "余额不足", http.StatusBadRequest)
		return
	}

	var roundID int
	tx.QueryRow("SELECT current_round_id FROM game_state").Scan(&roundID)

	_, err := tx.Exec("UPDATE users SET balance = balance - ? WHERE id = ?", input.Amount, userID)
	if err != nil {
		return
	}

	_, err = tx.Exec("INSERT INTO bets (user_id, amount, round_id) VALUES (?, ?, ?)", userID, input.Amount, roundID)
	if err != nil {
		return
	}

	tx.Commit()
	w.WriteHeader(http.StatusOK)
}

func ResetBalanceHandler(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value("user_id").(int)
	var balance int
	db.QueryRow("SELECT balance FROM users WHERE id = ?", userID).Scan(&balance)
	if balance < 1000 {
		db.Exec("UPDATE users SET balance = 1000 WHERE id = ?", userID)
		w.WriteHeader(http.StatusOK)
	} else {
		http.Error(w, "Balance already above 1000", http.StatusBadRequest)
	}
}

func GetGameStateHandler(w http.ResponseWriter, r *http.Request) {
	var nextRes time.Time
	var roundID int
	db.QueryRow("SELECT current_round_id, next_resolution_at FROM game_state").Scan(&roundID, &nextRes)

	rows, _ := db.Query("SELECT outcome, timestamp FROM history ORDER BY timestamp DESC LIMIT 10")
	var history []map[string]interface{}
	for rows.Next() {
		var outcome string
		var t time.Time
		rows.Scan(&outcome, &t)
		history = append(history, map[string]interface{}{"outcome": outcome, "time": t})
	}

	rows, _ = db.Query("SELECT COALESCE(username, SUBSTR(email, 1, INSTR(email, '@')-1)), balance FROM users ORDER BY balance DESC LIMIT 10")
	var leaderboard []map[string]interface{}
	for rows.Next() {
		var name string
		var balance int
		rows.Scan(&name, &balance)
		leaderboard = append(leaderboard, map[string]interface{}{"name": name, "balance": balance})
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"round_id":          roundID,
		"next_res_at":       nextRes,
		"history":           history,
		"leaderboard":       leaderboard,
		"current_time":      time.Now(),
		"turnstile_sitekey": os.Getenv("TURNSTILE_SITEKEY"),
	})
}
