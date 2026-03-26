package main

import (
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/joho/godotenv"
)

func main() {
	godotenv.Load()
	initDB()

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RealIP)

	r.Route("/api", func(r chi.Router) {
		r.Post("/auth/register", RegisterHandler)
		r.Post("/auth/verify", VerifyHandler)
		r.Get("/game/state", GetGameStateHandler)

		r.Group(func(r chi.Router) {
			r.Use(AuthMiddleware)
			r.Get("/me", GetMeHandler)
			r.Post("/bet", PlaceBetHandler)
			r.Post("/reset", ResetBalanceHandler)
		})
	})

	// Serve static files
	workDir, _ := os.Getwd()
	filesDir := http.Dir(fmt.Sprintf("%s/public", workDir))
	r.Handle("/*", http.FileServer(filesDir))

	go gameLoop()

	port := os.Getenv("PORT")
	if port == "" {
		port = "4444"
	}
	fmt.Printf("Server starting on port %s\n", port)
	http.ListenAndServe(":"+port, r)
}

func gameLoop() {
	ticker := time.NewTicker(10 * time.Second)
	for range ticker.C {
		resolveRound()
	}
}

func resolveRound() {
	tx, err := db.Begin()
	if err != nil {
		fmt.Printf("Error starting transaction: %v\n", err)
		return
	}
	defer tx.Rollback()

	var roundID int
	err = tx.QueryRow("SELECT current_round_id FROM game_state WHERE id = 1").Scan(&roundID)
	if err != nil {
		return
	}

	outcome := "win"
	if rand.Intn(2) == 0 {
		outcome = "lose"
	}

	// Update bets and balances
	if outcome == "win" {
		_, err = tx.Exec(`
			UPDATE users
			SET balance = balance + (SELECT SUM(amount) FROM bets WHERE user_id = users.id AND round_id = ? AND status = 'pending')
			WHERE id IN (SELECT user_id FROM bets WHERE round_id = ? AND status = 'pending')
		`, roundID, roundID)
		_, err = tx.Exec("UPDATE bets SET status = 'won' WHERE round_id = ? AND status = 'pending'", roundID)
	} else {
		_, err = tx.Exec("UPDATE bets SET status = 'lost' WHERE round_id = ? AND status = 'pending'", roundID)
	}

	// Insert into history
	_, err = tx.Exec("INSERT INTO history (outcome) VALUES (?)", outcome)

	// Update game state for next round
	nextResat := time.Now().Add(10 * time.Second)
	_, err = tx.Exec("UPDATE game_state SET current_round_id = current_round_id + 1, next_resolution_at = ? WHERE id = 1", nextResat)

	tx.Commit()
	fmt.Printf("Round %d resolved: %s\n", roundID, outcome)
}
