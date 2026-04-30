package db

import (
	"database/sql"
	"encoding/json"
	"io"
	"log"
	"os"
	"time"

	"github.com/google/uuid"
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

	// Optional seeding: set SEED_ON_START=true and optionally SEED_FILE to a path
	if os.Getenv("SEED_ON_START") == "true" {
		seedFile := os.Getenv("SEED_FILE")
		if seedFile == "" {
			seedFile = "/app/data/seeds/1seed.json"
		}
		if err := maybeSeed(seedFile); err != nil {
			log.Printf("db: seed warning: %v", err)
		}
	}
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

	-- meta table to track applied seeds
	CREATE TABLE IF NOT EXISTS seed_applied (
		filename TEXT PRIMARY KEY,
		applied_at TIMESTAMP NOT NULL
	);
	`
	if _, err := DB.Exec(schema); err != nil {
		log.Fatalf("db: migrate: %v", err)
	}
}

// maybeSeed checks the seed_applied table and only runs seedFromFile if
// the given seed file has not been applied yet. This prevents re-applying
// the same seed on container restarts.
func maybeSeed(path string) error {
	// If the seed file doesn't exist, nothing to do.
	if _, err := os.Stat(path); err != nil {
		return err
	}
	// Use the filename as the key
	filename := path
	// Check if applied
	var exists int
	err := DB.QueryRow("SELECT 1 FROM seed_applied WHERE filename = ?", filename).Scan(&exists)
	if err == nil {
		// already applied
		log.Printf("db: seed %s already applied, skipping", filename)
		return nil
	}
	if err != sql.ErrNoRows {
		// some other error
		// continue to attempt seeding
		log.Printf("db: seed check error: %v", err)
	}

	// Apply the seed
	if err := seedFromFile(path); err != nil {
		return err
	}
	// Mark applied
	if _, err := DB.Exec("INSERT OR REPLACE INTO seed_applied (filename, applied_at) VALUES (?, ?)", filename, time.Now().UTC()); err != nil {
		return err
	}
	log.Printf("db: seed %s applied", filename)
	return nil
}

// seedFromFile reads a JSON file with optional `users` and `profiles` arrays
// and inserts or updates rows idempotently. The JSON shape:
// { "users": [{"github_id":"...","username":"...","email":"...","role":"admin"}, ...], "profiles": [...] }
func seedFromFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	if err != nil {
		return err
	}
	var payload struct {
		Users []struct {
			GithubID string `json:"github_id"`
			Username string `json:"username"`
			Email    string `json:"email"`
			Avatar   string `json:"avatar_url"`
			Role     string `json:"role"`
		}
		Profiles []struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Gender  string `json:"gender"`
			Age     int    `json:"age"`
			Country string `json:"country_id"`
		}
	}
	if err := json.Unmarshal(b, &payload); err != nil {
		return err
	}

	// Upsert users
	for _, u := range payload.Users {
		// Check if user exists by github_id
		var id string
		err := DB.QueryRow("SELECT id FROM users WHERE github_id = ?", u.GithubID).Scan(&id)
		if err == sql.ErrNoRows {
			// Insert
			newID := uuid.NewString()
			if _, err := DB.Exec(`INSERT INTO users (id, github_id, username, email, avatar_url, role, is_active, created_at) VALUES (?, ?, ?, ?, ?, ?, 1, CURRENT_TIMESTAMP)`, newID, u.GithubID, u.Username, u.Email, u.Avatar, u.Role); err != nil {
				log.Printf("db: seed insert user %s: %v", u.GithubID, err)
			}
		} else if err != nil {
			log.Printf("db: seed select user %s: %v", u.GithubID, err)
		} else {
			// Update existing row to ensure fields are present
			if _, err := DB.Exec("UPDATE users SET username = ?, email = ?, avatar_url = ?, role = ? WHERE github_id = ?", u.Username, u.Email, u.Avatar, u.Role, u.GithubID); err != nil {
				log.Printf("db: seed update user %s: %v", u.GithubID, err)
			}
		}
	}

	// Upsert profiles (minimal)
	for _, p := range payload.Profiles {
		if _, err := DB.Exec(`INSERT OR IGNORE INTO profiles (id, name, gender, age, country_id, created_at) VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`, p.ID, p.Name, p.Gender, p.Age, p.Country); err != nil {
			log.Printf("db: seed insert profile %s: %v", p.Name, err)
		}
	}

	return nil
}
