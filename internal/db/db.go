package db

import (
	"database/sql"
	"log"
	"os"

	_ "github.com/mattn/go-sqlite3"
)

var DB *sql.DB

func Init() {
	path := os.Getenv("DB_PATH")
	if path == "" {
		path = "./insighta.db"
	}

	var err error
	// WAL mode improves concurrent read performance
	DB, err = sql.Open("sqlite3", path+"?_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		log.Fatalf("db: open: %v", err)
	}

	if err = DB.Ping(); err != nil {
		log.Fatalf("db: ping: %v", err)
	}

	migrate()
}

func migrate() {
	schema := `
	CREATE TABLE IF NOT EXISTS profiles (
		id                  TEXT PRIMARY KEY,
		name                TEXT UNIQUE NOT NULL,
		gender              TEXT,
		gender_probability  REAL,
		age                 INTEGER,
		age_group           TEXT,
		country_id          TEXT,
		country_name        TEXT,
		country_probability REAL,
		created_at          TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_profiles_filters ON profiles(gender, age, country_id, age_group);

	CREATE TABLE IF NOT EXISTS users (
		id            TEXT PRIMARY KEY,
		github_id     TEXT UNIQUE NOT NULL,
		username      TEXT NOT NULL,
		email         TEXT,
		avatar_url    TEXT,
		role          TEXT NOT NULL DEFAULT 'analyst',
		is_active     INTEGER NOT NULL DEFAULT 1,
		last_login_at TIMESTAMP,
		created_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS refresh_tokens (
		id         TEXT PRIMARY KEY,
		user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		token_hash TEXT NOT NULL UNIQUE,
		expires_at TIMESTAMP NOT NULL,
		used_at    TIMESTAMP,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user ON refresh_tokens(user_id);
	`
	if _, err := DB.Exec(schema); err != nil {
		log.Fatalf("db: migrate: %v", err)
	}
}
