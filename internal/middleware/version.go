package middleware

import (
	"encoding/json"
	"net/http"

	"insighta-backend/internal/models"
)

// RequireAPIVersion rejects requests that do not carry the
// X-API-Version: 1 header with a 400 response.
func RequireAPIVersion(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Version") != "1" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(models.APIResponse{
				Status:  "error",
				Message: "API version header required",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}
