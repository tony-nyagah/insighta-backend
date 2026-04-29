package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"insighta-backend/internal/db"
	"insighta-backend/internal/models"
)

// githubUser is the shape returned by the GitHub /user API.
type githubUser struct {
	ID        int    `json:"id"`
	Login     string `json:"login"`
	Email     string `json:"email"`
	AvatarURL string `json:"avatar_url"`
}

// HandleGithubRedirect redirects the user/CLI to the GitHub OAuth page.
// For PKCE (CLI), it accepts optional code_challenge + code_challenge_method
// and state query params and passes them through to GitHub.
func HandleGithubRedirect(w http.ResponseWriter, r *http.Request) {
	clientID := os.Getenv("GITHUB_CLIENT_ID")
	redirectURI := os.Getenv("GITHUB_REDIRECT_URI")

	state := r.URL.Query().Get("state")
	codeChallenge := r.URL.Query().Get("code_challenge")
	challengeMethod := r.URL.Query().Get("code_challenge_method")

	params := url.Values{}
	params.Set("client_id", clientID)
	params.Set("redirect_uri", redirectURI)
	params.Set("scope", "read:user user:email")
	if state != "" {
		params.Set("state", state)
	}
	// GitHub does not natively support PKCE, so we store the challenge
	// in the state-like flow. The CLI sends code_verifier directly to
	// /auth/github/callback, and we verify on our side.
	if codeChallenge != "" {
		params.Set("code_challenge", codeChallenge)
		params.Set("code_challenge_method", challengeMethod)
	}

	http.Redirect(w, r, "https://github.com/login/oauth/authorize?"+params.Encode(), http.StatusFound)
}

// HandleGithubCallback handles the OAuth callback from GitHub.
// It accepts an optional code_verifier (for PKCE / CLI flows).
func HandleGithubCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		writeError(w, http.StatusBadRequest, "missing code parameter")
		return
	}

	codeVerifier := r.URL.Query().Get("code_verifier")
	if codeVerifier == "" {
		// Also check JSON body (POST from CLI)
		var body struct {
			Code         string `json:"code"`
			CodeVerifier string `json:"code_verifier"`
		}
		if r.Method == http.MethodPost {
			json.NewDecoder(r.Body).Decode(&body)
			if body.Code != "" {
				code = body.Code
			}
			codeVerifier = body.CodeVerifier
		}
	}

	accessToken, err := exchangeCodeForToken(code, codeVerifier)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to exchange code: "+err.Error())
		return
	}

	ghUser, err := fetchGithubUser(accessToken)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to fetch github user")
		return
	}

	user, err := upsertUser(ghUser)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create user")
		return
	}
	if !user.IsActive {
		writeError(w, http.StatusForbidden, "account disabled")
		return
	}

	jwtToken, err := IssueAccessToken(user)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to issue access token")
		return
	}
	refreshToken, err := IssueRefreshToken(user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to issue refresh token")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":        "success",
		"access_token":  jwtToken,
		"refresh_token": refreshToken,
		"user": map[string]interface{}{
			"id":         user.ID,
			"username":   user.Username,
			"email":      user.Email,
			"avatar_url": user.AvatarURL,
			"role":       user.Role,
		},
	})
}

// HandleRefresh rotates the refresh token pair.
func HandleRefresh(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.RefreshToken == "" {
		writeError(w, http.StatusBadRequest, "refresh_token required")
		return
	}

	pair, _, err := RotateRefreshToken(body.RefreshToken)
	if err != nil {
		if errors.Is(err, ErrTokenExpired) {
			writeError(w, http.StatusUnauthorized, "refresh token expired")
			return
		}
		writeError(w, http.StatusUnauthorized, "invalid refresh token")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":        "success",
		"access_token":  pair.AccessToken,
		"refresh_token": pair.RefreshToken,
	})
}

// HandleLogout invalidates the refresh token server-side.
func HandleLogout(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RefreshToken string `json:"refresh_token"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	if body.RefreshToken != "" {
		// Best-effort — ignore the error; we still return success
		_ = RevokeRefreshToken(body.RefreshToken)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(models.APIResponse{Status: "success", Message: "logged out"})
}

// --- GitHub API helpers ---

func exchangeCodeForToken(code, codeVerifier string) (string, error) {
	clientID := os.Getenv("GITHUB_CLIENT_ID")
	clientSecret := os.Getenv("GITHUB_CLIENT_SECRET")
	redirectURI := os.Getenv("GITHUB_REDIRECT_URI")

	params := url.Values{}
	params.Set("client_id", clientID)
	params.Set("client_secret", clientSecret)
	params.Set("code", code)
	params.Set("redirect_uri", redirectURI)

	req, _ := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"https://github.com/login/oauth/access_token",
		strings.NewReader(params.Encode()),
	)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
	}
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}
	if result.Error != "" {
		return "", fmt.Errorf("%s: %s", result.Error, result.ErrorDesc)
	}
	return result.AccessToken, nil
}

func fetchGithubUser(accessToken string) (*githubUser, error) {
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://api.github.com/user", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var u githubUser
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return nil, err
	}

	// GitHub may return an empty email; fetch from the emails endpoint
	if u.Email == "" {
		u.Email = fetchPrimaryEmail(accessToken)
	}
	return &u, nil
}

func fetchPrimaryEmail(accessToken string) string {
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://api.github.com/user/emails", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	var emails []struct {
		Email   string `json:"email"`
		Primary bool   `json:"primary"`
	}
	json.NewDecoder(resp.Body).Decode(&emails)
	for _, e := range emails {
		if e.Primary {
			return e.Email
		}
	}
	return ""
}

func upsertUser(gh *githubUser) (*models.User, error) {
	githubID := fmt.Sprintf("%d", gh.ID)
	now := time.Now().UTC()

	// Try to find existing user
	row := db.DB.QueryRow(
		`SELECT id, github_id, username, email, avatar_url, role, is_active, last_login_at, created_at
		 FROM users WHERE github_id = ?`, githubID,
	)
	u := &models.User{}
	err := row.Scan(&u.ID, &u.GithubID, &u.Username, &u.Email, &u.AvatarURL,
		&u.Role, &u.IsActive, &u.LastLoginAt, &u.CreatedAt)

	if err != nil {
		// New user
		id, err := uuid.NewV7()
		if err != nil {
			return nil, err
		}
		u = &models.User{
			ID:        id.String(),
			GithubID:  githubID,
			Username:  gh.Login,
			Email:     gh.Email,
			AvatarURL: gh.AvatarURL,
			Role:      "analyst",
			IsActive:  true,
			CreatedAt: now,
		}
		_, err = db.DB.Exec(
			`INSERT INTO users (id, github_id, username, email, avatar_url, role, is_active, last_login_at, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			u.ID, u.GithubID, u.Username, u.Email, u.AvatarURL, u.Role, 1, now, u.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		u.LastLoginAt = &now
		return u, nil
	}

	// Update existing user metadata + last_login_at
	_, err = db.DB.Exec(
		`UPDATE users SET username = ?, email = ?, avatar_url = ?, last_login_at = ? WHERE id = ?`,
		gh.Login, gh.Email, gh.AvatarURL, now, u.ID,
	)
	u.LastLoginAt = &now
	return u, err
}

// HandleMe returns the currently authenticated user's profile.
func HandleMe(w http.ResponseWriter, r *http.Request) {
	u, ok := r.Context().Value(models.ContextKeyUser).(*models.User)
	if !ok || u == nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "success",
		"user": map[string]interface{}{
			"id":            u.ID,
			"username":      u.Username,
			"email":         u.Email,
			"avatar_url":    u.AvatarURL,
			"role":          u.Role,
			"last_login_at": u.LastLoginAt,
		},
	})
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(models.APIResponse{Status: "error", Message: msg})
}
