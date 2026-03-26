package main

import (
	"database/sql"
	"log"
	"os"

	_ "modernc.org/sqlite"
)

var db *sql.DB

func initDB() {
	var err error
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "./game.db"
	}

	db, err = sql.Open("sqlite", dbPath)
	if err != nil {
		log.Fatal(err)
	}

	createTables := `
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		email TEXT UNIQUE,
		balance INTEGER DEFAULT 1000,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS otp_codes (
		email TEXT PRIMARY KEY,
		code TEXT,
		expires_at DATETIME
	);

	CREATE TABLE IF NOT EXISTS bets (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER,
		amount INTEGER,
		round_id INTEGER,
		status TEXT DEFAULT 'pending', -- pending, won, lost
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		outcome TEXT, -- win, lose
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS game_state (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		current_round_id INTEGER DEFAULT 1,
		next_resolution_at DATETIME
	);

	INSERT OR IGNORE INTO game_state (id, current_round_id, next_resolution_at)
	VALUES (1, 1, datetime('now', '+10 seconds'));
	`

	_, err = db.Exec(createTables)
	if err != nil {
		log.Fatal(err)
	}
}
