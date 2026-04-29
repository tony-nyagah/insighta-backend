package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"insighta-backend/internal/auth"
	"insighta-backend/internal/db"
	"insighta-backend/internal/middleware"
	"insighta-backend/internal/profiles"
)

func main() {
	// Load .env if present (simple key=value, no library needed for Docker)
	loadEnv(".env")

	db.Init()

	r := chi.NewRouter()

	// Global middleware
	r.Use(chimw.Recoverer)
	r.Use(middleware.Logger)

	// Auth routes — 10 req/min per IP
	r.Group(func(r chi.Router) {
		r.Use(middleware.RateLimit(10, middleware.IPKey))
		r.Get("/auth/github", auth.HandleGithubRedirect)
		r.Get("/auth/github/callback", auth.HandleGithubCallback)
		r.Post("/auth/github/callback", auth.HandleGithubCallback)
		r.Post("/auth/refresh", auth.HandleRefresh)
		r.Post("/auth/logout", auth.HandleLogout)
	})

	// API routes — authenticated, versioned, 60 req/min per user
	r.Group(func(r chi.Router) {
		r.Use(middleware.Authenticate)
		r.Use(middleware.RateLimit(60, middleware.UserKey))
		r.Use(middleware.RequireAPIVersion)

		// Auth: current user info
		r.Get("/auth/me", auth.HandleMe)

		// Analyst + admin: read
		r.Get("/api/profiles", profiles.ListProfiles)
		r.Get("/api/profiles/search", profiles.SearchProfiles)
		r.Get("/api/profiles/export", profiles.ExportProfiles)
		r.Get("/api/profiles/{id}", profiles.GetProfile)

		// Admin only: write
		r.With(middleware.RequireRole("admin")).Post("/api/profiles", profiles.CreateProfileHandler)
	})

	// Health check (no auth required)
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	fmt.Printf("insighta-backend listening on :%s\n", port)
	log.Fatal(http.ListenAndServe(":"+port, r))
}

// loadEnv reads a simple KEY=VALUE .env file. It does not override existing
// environment variables, so Docker / OS env always takes precedence.
func loadEnv(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return // .env is optional
	}
	for _, line := range splitLines(string(data)) {
		if line == "" || line[0] == '#' {
			continue
		}
		idx := indexOf(line, '=')
		if idx < 0 {
			continue
		}
		key := line[:idx]
		val := line[idx+1:]
		if os.Getenv(key) == "" {
			os.Setenv(key, val)
		}
	}
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func indexOf(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}
