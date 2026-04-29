package models

import "time"

type Profile struct {
	ID                 string    `json:"id"`
	Name               string    `json:"name"`
	Gender             string    `json:"gender"`
	GenderProbability  float64   `json:"gender_probability"`
	Age                int       `json:"age"`
	AgeGroup           string    `json:"age_group"`
	CountryID          string    `json:"country_id"`
	CountryName        string    `json:"country_name"`
	CountryProbability float64   `json:"country_probability"`
	CreatedAt          time.Time `json:"created_at"`
}

type User struct {
	ID          string     `json:"id"`
	GithubID    string     `json:"github_id"`
	Username    string     `json:"username"`
	Email       string     `json:"email"`
	AvatarURL   string     `json:"avatar_url"`
	Role        string     `json:"role"` // "admin" | "analyst"
	IsActive    bool       `json:"is_active"`
	LastLoginAt *time.Time `json:"last_login_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

type RefreshToken struct {
	ID        string     `json:"id"`
	UserID    string     `json:"user_id"`
	TokenHash string     `json:"-"`
	ExpiresAt time.Time  `json:"expires_at"`
	UsedAt    *time.Time `json:"used_at"`
	CreatedAt time.Time  `json:"created_at"`
}

// API response shapes

type APIResponse struct {
	Status  string      `json:"status"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
}

type PaginatedResponse struct {
	Status     string      `json:"status"`
	Page       int         `json:"page"`
	Limit      int         `json:"limit"`
	Total      int         `json:"total"`
	TotalPages int         `json:"total_pages"`
	Links      PageLinks   `json:"links"`
	Data       interface{} `json:"data"`
}

type PageLinks struct {
	Self string  `json:"self"`
	Next *string `json:"next"`
	Prev *string `json:"prev"`
}

type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

// Context key types — avoids string key collisions
type ContextKey string

const (
	ContextKeyUser ContextKey = "user"
)
