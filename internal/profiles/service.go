package profiles

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"insighta-backend/internal/db"
	"insighta-backend/internal/models"
)

// --- External API types ---

type genderizeResponse struct {
	Name        string  `json:"name"`
	Gender      string  `json:"gender"`
	Probability float64 `json:"probability"`
}

type agifyResponse struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

type nationalizeCountry struct {
	CountryID   string  `json:"country_id"`
	Probability float64 `json:"probability"`
}

type nationalizeResponse struct {
	Name    string               `json:"name"`
	Country []nationalizeCountry `json:"country"`
}

// ageGroup maps an age to the appropriate label.
func ageGroup(age int) string {
	switch {
	case age < 13:
		return "child"
	case age < 18:
		return "teenager"
	case age < 60:
		return "adult"
	default:
		return "senior"
	}
}

var countryNames = map[string]string{
	"NG": "Nigeria", "KE": "Kenya", "AO": "Angola", "BJ": "Benin",
	"GH": "Ghana", "ZA": "South Africa", "US": "United States",
	"GB": "United Kingdom", "DE": "Germany", "FR": "France",
	"IN": "India", "BR": "Brazil", "CA": "Canada", "AU": "Australia",
	"JP": "Japan", "CN": "China", "MX": "Mexico", "IT": "Italy",
	"ES": "Spain", "RU": "Russia",
}

func countryName(code string) string {
	if name, ok := countryNames[code]; ok {
		return name
	}
	return code
}

// CreateProfile calls external APIs and persists the result.
func CreateProfile(name string) (*models.Profile, error) {
	genderAPIBase := os.Getenv("GENDERIZE_API")
	agifyAPIBase := os.Getenv("AGIFY_API")
	nationalizeAPIBase := os.Getenv("NATIONALIZE_API")

	if genderAPIBase == "" {
		genderAPIBase = "https://api.genderize.io"
	}
	if agifyAPIBase == "" {
		agifyAPIBase = "https://api.agify.io"
	}
	if nationalizeAPIBase == "" {
		nationalizeAPIBase = "https://api.nationalize.io"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Fetch gender
	genderData, err := fetchJSON[genderizeResponse](ctx, genderAPIBase+"?name="+name)
	if err != nil {
		return nil, fmt.Errorf("genderize: %w", err)
	}

	// Fetch age
	agifyData, err := fetchJSON[agifyResponse](ctx, agifyAPIBase+"?name="+name)
	if err != nil {
		return nil, fmt.Errorf("agify: %w", err)
	}

	// Fetch nationality
	natData, err := fetchJSON[nationalizeResponse](ctx, nationalizeAPIBase+"?name="+name)
	if err != nil {
		return nil, fmt.Errorf("nationalize: %w", err)
	}

	var topCountryID string
	var topCountryProb float64
	if len(natData.Country) > 0 {
		topCountryID = natData.Country[0].CountryID
		topCountryProb = natData.Country[0].Probability
	}

	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()

	p := &models.Profile{
		ID:                 id.String(),
		Name:               name,
		Gender:             genderData.Gender,
		GenderProbability:  genderData.Probability,
		Age:                agifyData.Age,
		AgeGroup:           ageGroup(agifyData.Age),
		CountryID:          topCountryID,
		CountryName:        countryName(topCountryID),
		CountryProbability: topCountryProb,
		CreatedAt:          now,
	}

	_, err = db.DB.Exec(
		`INSERT INTO profiles (id, name, gender, gender_probability, age, age_group, country_id, country_name, country_probability, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Name, p.Gender, p.GenderProbability, p.Age, p.AgeGroup,
		p.CountryID, p.CountryName, p.CountryProbability, p.CreatedAt,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return nil, fmt.Errorf("profile with name %q already exists", name)
		}
		return nil, err
	}
	return p, nil
}

func fetchJSON[T any](ctx context.Context, url string) (T, error) {
	var zero T
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return zero, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return zero, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result T
	if err := json.Unmarshal(body, &result); err != nil {
		return zero, err
	}
	return result, nil
}
