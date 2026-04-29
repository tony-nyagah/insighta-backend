package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"insighta-backend/internal/auth"
	"insighta-backend/internal/db"
	"insighta-backend/internal/models"
)

// Authenticate validates the Bearer JWT and injects the *models.User into
// the request context. Returns 401 if the token is missing or invalid,
// 403 if the account is disabled.
func Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			writeAuthError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		tokenStr := strings.TrimPrefix(header, "Bearer ")

		claims, err := auth.ParseAccessToken(tokenStr)
		if err != nil {
			if err == auth.ErrTokenExpired {
				writeAuthError(w, http.StatusUnauthorized, "access token expired")
				return
			}
			writeAuthError(w, http.StatusUnauthorized, "invalid token")
			return
		}

		// Load fresh user row to catch is_active changes
		row := db.DB.QueryRow(
			`SELECT id, github_id, username, email, avatar_url, role, is_active, last_login_at, created_at
			 FROM users WHERE id = ?`, claims.UserID,
		)
		u := &models.User{}
		if err := row.Scan(&u.ID, &u.GithubID, &u.Username, &u.Email, &u.AvatarURL,
			&u.Role, &u.IsActive, &u.LastLoginAt, &u.CreatedAt); err != nil {
			writeAuthError(w, http.StatusUnauthorized, "user not found")
			return
		}
		if !u.IsActive {
			writeAuthError(w, http.StatusForbidden, "account disabled")
			return
		}

		ctx := context.WithValue(r.Context(), models.ContextKeyUser, u)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireRole returns a middleware that enforces a minimum role.
// Roles: "admin" > "analyst". admin can do everything; analyst is read-only.
func RequireRole(role string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u, ok := r.Context().Value(models.ContextKeyUser).(*models.User)
			if !ok || u == nil {
				writeAuthError(w, http.StatusUnauthorized, "authentication required")
				return
			}
			if !hasRole(u.Role, role) {
				writeAuthError(w, http.StatusForbidden, "insufficient permissions")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// hasRole returns true if userRole satisfies the required role.
func hasRole(userRole, required string) bool {
	if userRole == "admin" {
		return true // admin passes everything
	}
	return userRole == required
}

func writeAuthError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(models.APIResponse{Status: "error", Message: msg})
}
