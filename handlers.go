package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/mail"
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

	// Validate email format.
	if _, err := mail.ParseAddress(input.Email); err != nil {
		http.Error(w, "邮箱格式无效", http.StatusBadRequest)
		return
	}

	if !VerifyTurnstile(input.TurnstileToken) {
		http.Error(w, "安全验证未通过", http.StatusForbidden)
		return
	}

	code, err := GenerateOTP()
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	expires := time.Now().Add(5 * time.Minute)

	_, err = db.Exec("INSERT OR REPLACE INTO otp_codes (email, code, expires_at) VALUES (?, ?, ?)", input.Email, code, expires)
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	if err := SendOTP(input.Email, code); err != nil {
		fmt.Printf("SendOTP error: %v\n", err)
		http.Error(w, "发送验证码失败", http.StatusInternalServerError)
		return
	}
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

	// Check OTP rate limit before querying DB.
	if CheckOTPRateLimit(input.Email) {
		http.Error(w, "验证尝试次数过多，请稍后再试", http.StatusTooManyRequests)
		return
	}

	var storedCode string
	var expiresAt time.Time
	err := db.QueryRow("SELECT code, expires_at FROM otp_codes WHERE email = ?", input.Email).Scan(&storedCode, &expiresAt)
	if err != nil || storedCode != input.Code || time.Now().After(expiresAt) {
		RecordOTPFailure(input.Email)
		http.Error(w, "验证码无效或已过期", http.StatusUnauthorized)
		return
	}

	// Delete OTP after successful verification to prevent reuse.
	db.Exec("DELETE FROM otp_codes WHERE email = ?", input.Email)
	ClearOTPAttempts(input.Email)

	// Create user if not exists
	_, err = db.Exec("INSERT OR IGNORE INTO users (email) VALUES (?)", input.Email)
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	var userID int
	if err := db.QueryRow("SELECT id FROM users WHERE email = ?", input.Email).Scan(&userID); err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// Set JWT Cookie with security attributes.
	expirationTime := time.Now().Add(24 * time.Hour)
	claims := &Claims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expirationTime),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(jwtKey)
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "token",
		Value:    tokenString,
		Expires:  expirationTime,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})

	w.WriteHeader(http.StatusOK)
}

func GetMeHandler(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(ctxUserID).(int)
	var balance int
	var email, username sql.NullString
	if err := db.QueryRow("SELECT email, username, balance FROM users WHERE id = ?", userID).Scan(&email, &username, &balance); err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"email":    email.String,
		"username": username.String,
		"balance":  balance,
	})
}

func UpdateUsernameHandler(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(ctxUserID).(int)
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
	userID := r.Context().Value(ctxUserID).(int)
	var input struct {
		Amount int `json:"amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	if input.Amount <= 0 {
		http.Error(w, "请输入有效的投注金额", http.StatusBadRequest)
		return
	}

	// Use gameMu to prevent race between bet placement and round resolution.
	gameMu.Lock()
	defer gameMu.Unlock()

	tx, err := db.Begin()
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	var balance int
	if err := tx.QueryRow("SELECT balance FROM users WHERE id = ?", userID).Scan(&balance); err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	if balance < input.Amount {
		http.Error(w, "余额不足", http.StatusBadRequest)
		return
	}

	var roundID int
	if err := tx.QueryRow("SELECT current_round_id FROM game_state WHERE id = 1").Scan(&roundID); err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// Check for duplicate bet in this round.
	var existingBet int
	err = tx.QueryRow("SELECT COUNT(*) FROM bets WHERE user_id = ? AND round_id = ? AND status = 'pending'", userID, roundID).Scan(&existingBet)
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	if existingBet > 0 {
		http.Error(w, "本轮已投注，请等待结算", http.StatusBadRequest)
		return
	}

	if _, err := tx.Exec("UPDATE users SET balance = balance - ? WHERE id = ?", input.Amount, userID); err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	if _, err := tx.Exec("INSERT INTO bets (user_id, amount, round_id) VALUES (?, ?, ?)", userID, input.Amount, roundID); err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func ResetBalanceHandler(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(ctxUserID).(int)

	// Atomic check-and-update to prevent TOCTOU race condition.
	result, err := db.Exec("UPDATE users SET balance = 1000 WHERE id = ? AND balance < 1000", userID)
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	rows, err := result.RowsAffected()
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	if rows == 0 {
		http.Error(w, "Balance already above 1000", http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func GetGameStateHandler(w http.ResponseWriter, r *http.Request) {
	var nextRes time.Time
	var roundID int
	if err := db.QueryRow("SELECT current_round_id, next_resolution_at FROM game_state WHERE id = 1").Scan(&roundID, &nextRes); err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	rows, err := db.Query("SELECT outcome, timestamp FROM history ORDER BY timestamp DESC LIMIT 10")
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var history []map[string]interface{}
	for rows.Next() {
		var outcome string
		var t time.Time
		if err := rows.Scan(&outcome, &t); err != nil {
			continue
		}
		history = append(history, map[string]interface{}{"outcome": outcome, "time": t})
	}

	lbRows, err := db.Query("SELECT COALESCE(username, SUBSTR(email, 1, INSTR(email, '@')-1)), balance FROM users ORDER BY balance DESC LIMIT 10")
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	defer lbRows.Close()

	var leaderboard []map[string]interface{}
	for lbRows.Next() {
		var name string
		var balance int
		if err := lbRows.Scan(&name, &balance); err != nil {
			continue
		}
		leaderboard = append(leaderboard, map[string]interface{}{"name": name, "balance": balance})
	}

	// Current round's pending bets.
	betRows, err := db.Query(`
		SELECT COALESCE(u.username, SUBSTR(u.email, 1, INSTR(u.email, '@')-1)), b.amount
		FROM bets b JOIN users u ON b.user_id = u.id
		WHERE b.round_id = ? AND b.status = 'pending'
		ORDER BY b.amount DESC`, roundID)
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	defer betRows.Close()

	var currentBets []map[string]interface{}
	for betRows.Next() {
		var name string
		var amount int
		if err := betRows.Scan(&name, &amount); err != nil {
			continue
		}
		currentBets = append(currentBets, map[string]interface{}{"name": name, "amount": amount})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"round_id":          roundID,
		"next_res_at":       nextRes,
		"history":           history,
		"leaderboard":       leaderboard,
		"current_bets":      currentBets,
		"current_time":      time.Now(),
		"turnstile_sitekey": os.Getenv("TURNSTILE_SITEKEY"),
	})
}
