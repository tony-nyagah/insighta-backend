package auth

import (
	"errors"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"insighta-backend/internal/db"
	"insighta-backend/internal/models"
)

var ErrTokenExpired = errors.New("token expired")
var ErrTokenInvalid = errors.New("token invalid")

type Claims struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

func jwtSecret() []byte {
	s := os.Getenv("JWT_SECRET")
	if s == "" {
		panic("JWT_SECRET env var not set")
	}
	return []byte(s)
}

// IssueAccessToken mints a signed JWT valid for 3 minutes.
func IssueAccessToken(user *models.User) (string, error) {
	now := time.Now().UTC()
	claims := Claims{
		UserID:   user.ID,
		Username: user.Username,
		Role:     user.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   user.ID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(3 * time.Minute)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(jwtSecret())
}

// ParseAccessToken validates and parses a JWT. Returns typed errors for
// expired vs invalid tokens so callers can return the correct HTTP status.
func ParseAccessToken(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrTokenInvalid
		}
		return jwtSecret(), nil
	})
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrTokenExpired
		}
		return nil, ErrTokenInvalid
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, ErrTokenInvalid
	}
	return claims, nil
}

// IssueRefreshToken generates a random opaque token, persists its hash in the
// DB, and returns the raw token (to be sent to the client once).
func IssueRefreshToken(userID string) (string, error) {
	raw, err := GenerateOpaqueToken()
	if err != nil {
		return "", err
	}
	hash := HashToken(raw)
	id, err := uuid.NewV7()
	if err != nil {
		return "", err
	}
	expiresAt := time.Now().UTC().Add(5 * time.Minute)

	_, err = db.DB.Exec(
		`INSERT INTO refresh_tokens (id, user_id, token_hash, expires_at) VALUES (?, ?, ?, ?)`,
		id.String(), userID, hash, expiresAt,
	)
	if err != nil {
		return "", err
	}
	return raw, nil
}

// RotateRefreshToken validates the provided raw token, marks it as used, issues
// a new pair, and returns them. Returns an error if the token is invalid,
// already used, or expired.
func RotateRefreshToken(rawToken string) (*models.TokenPair, *models.User, error) {
	hash := HashToken(rawToken)
	now := time.Now().UTC()

	row := db.DB.QueryRow(
		`SELECT id, user_id, expires_at, used_at FROM refresh_tokens WHERE token_hash = ?`,
		hash,
	)

	var id, userID string
	var expiresAt time.Time
	var usedAt *time.Time
	if err := row.Scan(&id, &userID, &expiresAt, &usedAt); err != nil {
		return nil, nil, ErrTokenInvalid
	}
	if usedAt != nil {
		return nil, nil, errors.New("refresh token already used")
	}
	if now.After(expiresAt) {
		return nil, nil, ErrTokenExpired
	}

	// Invalidate the used token
	if _, err := db.DB.Exec(`UPDATE refresh_tokens SET used_at = ? WHERE id = ?`, now, id); err != nil {
		return nil, nil, err
	}

	// Load the user
	user, err := getUserByID(userID)
	if err != nil {
		return nil, nil, err
	}
	if !user.IsActive {
		return nil, nil, errors.New("account disabled")
	}

	// Issue new pair
	accessToken, err := IssueAccessToken(user)
	if err != nil {
		return nil, nil, err
	}
	newRefresh, err := IssueRefreshToken(userID)
	if err != nil {
		return nil, nil, err
	}

	return &models.TokenPair{
		AccessToken:  accessToken,
		RefreshToken: newRefresh,
	}, user, nil
}

// RevokeRefreshToken marks a raw refresh token as used without issuing a new
// one (used on logout).
func RevokeRefreshToken(rawToken string) error {
	hash := HashToken(rawToken)
	now := time.Now().UTC()
	res, err := db.DB.Exec(
		`UPDATE refresh_tokens SET used_at = ? WHERE token_hash = ? AND used_at IS NULL`,
		now, hash,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errors.New("token not found or already revoked")
	}
	return nil
}

func getUserByID(id string) (*models.User, error) {
	row := db.DB.QueryRow(
		`SELECT id, github_id, username, email, avatar_url, role, is_active, last_login_at, created_at
		 FROM users WHERE id = ?`, id,
	)
	u := &models.User{}
	return u, row.Scan(&u.ID, &u.GithubID, &u.Username, &u.Email, &u.AvatarURL,
		&u.Role, &u.IsActive, &u.LastLoginAt, &u.CreatedAt)
}
