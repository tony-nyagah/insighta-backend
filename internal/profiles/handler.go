package profiles

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"insighta-backend/internal/db"
	"insighta-backend/internal/models"
)

// buildQuery constructs a parameterised SQL fragment and arg slice from a
// filters map. Allowed keys mirror the URL query params.
func buildQuery(filters map[string]string) (string, []interface{}) {
	base := "FROM profiles WHERE 1=1"
	var args []interface{}

	specs := map[string]string{
		"gender":                  "AND gender = ?",
		"age_group":               "AND age_group = ?",
		"country_id":              "AND country_id = ?",
		"min_age":                 "AND age >= ?",
		"max_age":                 "AND age <= ?",
		"min_gender_probability":  "AND gender_probability >= ?",
		"min_country_probability": "AND country_probability >= ?",
	}
	for k, clause := range specs {
		if v, ok := filters[k]; ok {
			base += " " + clause
			args = append(args, v)
		}
	}
	return base, args
}

// --- NLP parser (carried over from Stage 2, extended) ---

func parseNLP(q string) (map[string]string, bool) {
	q = strings.ToLower(q)
	f := make(map[string]string)
	found := false

	if strings.Contains(q, "female") {
		f["gender"] = "female"
		found = true
	} else if strings.Contains(q, "male") {
		f["gender"] = "male"
		found = true
	}

	if strings.Contains(q, "young") {
		f["min_age"], f["max_age"] = "16", "24"
		found = true
	}
	if strings.Contains(q, "teenager") {
		f["age_group"] = "teenager"
		found = true
	}
	if strings.Contains(q, "adult") {
		f["age_group"] = "adult"
		found = true
	}
	if strings.Contains(q, "senior") {
		f["age_group"] = "senior"
		found = true
	}
	if strings.Contains(q, "child") {
		f["age_group"] = "child"
		found = true
	}

	words := strings.Fields(q)
	for i, w := range words {
		if w == "above" && i+1 < len(words) {
			if val, err := strconv.Atoi(words[i+1]); err == nil {
				f["min_age"] = strconv.Itoa(val + 1)
				found = true
			}
		}
		if w == "under" && i+1 < len(words) {
			if val, err := strconv.Atoi(words[i+1]); err == nil {
				f["max_age"] = strconv.Itoa(val - 1)
				found = true
			}
		}
	}

	countries := map[string]string{
		"nigeria": "NG", "kenya": "KE", "angola": "AO", "benin": "BJ",
		"ghana": "GH", "south africa": "ZA", "united states": "US",
		"united kingdom": "GB", "germany": "DE", "france": "FR",
	}
	for name, code := range countries {
		if strings.Contains(q, name) {
			f["country_id"] = code
			found = true
		}
	}

	return f, found
}

// --- Handlers ---

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	writeJSON(w, models.APIResponse{Status: "error", Message: msg})
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	encodeJSON(w, v)
}

func encodeJSON(w http.ResponseWriter, v interface{}) {
	// using a proper encoder so we don't buffer the whole response
	enc := newJSONEncoder(w)
	enc.Encode(v)
}

// ListProfiles handles GET /api/profiles
func ListProfiles(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")

	params := r.URL.Query()
	filters := extractFilters(params, "gender", "age_group", "country_id",
		"min_age", "max_age", "min_gender_probability", "min_country_probability")

	respondPaginated(w, r, filters, "/api/profiles")
}

// SearchProfiles handles GET /api/profiles/search?q=...
func SearchProfiles(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")

	q := r.URL.Query().Get("q")
	if q == "" {
		writeError(w, http.StatusBadRequest, "missing or empty parameter")
		return
	}
	filters, ok := parseNLP(q)
	if !ok {
		writeError(w, http.StatusBadRequest, "unable to interpret query")
		return
	}
	respondPaginated(w, r, filters, "/api/profiles/search")
}

// GetProfile handles GET /api/profiles/{id}
func GetProfile(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// chi puts URL params in the context; extract manually for now
	parts := strings.Split(r.URL.Path, "/")
	id := parts[len(parts)-1]
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing id")
		return
	}

	var p models.Profile
	err := db.DB.QueryRow(
		`SELECT id, name, gender, gender_probability, age, age_group, country_id, country_name, country_probability, created_at
		 FROM profiles WHERE id = ?`, id,
	).Scan(&p.ID, &p.Name, &p.Gender, &p.GenderProbability, &p.Age, &p.AgeGroup,
		&p.CountryID, &p.CountryName, &p.CountryProbability, &p.CreatedAt)
	if err != nil {
		writeError(w, http.StatusNotFound, "profile not found")
		return
	}

	writeJSON(w, models.APIResponse{Status: "success", Data: p})
}

// CreateProfileHandler handles POST /api/profiles (admin only)
func CreateProfileHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &body); err != nil || strings.TrimSpace(body.Name) == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	p, err := CreateProfile(strings.TrimSpace(body.Name))
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeError(w, http.StatusBadGateway, "failed to create profile: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, models.APIResponse{Status: "success", Data: p})
}

// ExportProfiles handles GET /api/profiles/export?format=csv
func ExportProfiles(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("format") != "csv" {
		writeError(w, http.StatusBadRequest, "only format=csv is supported")
		return
	}

	params := r.URL.Query()
	filters := extractFilters(params, "gender", "age_group", "country_id",
		"min_age", "max_age", "min_gender_probability", "min_country_probability")

	sortBy, order := sortParams(params)

	base, args := buildQuery(filters)
	query := fmt.Sprintf("SELECT id, name, gender, gender_probability, age, age_group, country_id, country_name, country_probability, created_at %s ORDER BY %s %s", base, sortBy, order)

	rows, err := db.DB.Query(query, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	ts := time.Now().UTC().Format("20060102T150405Z")
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="profiles_%s.csv"`, ts))

	cw := csv.NewWriter(w)
	cw.Write([]string{"id", "name", "gender", "gender_probability", "age", "age_group", "country_id", "country_name", "country_probability", "created_at"})

	for rows.Next() {
		var p models.Profile
		rows.Scan(&p.ID, &p.Name, &p.Gender, &p.GenderProbability, &p.Age, &p.AgeGroup,
			&p.CountryID, &p.CountryName, &p.CountryProbability, &p.CreatedAt)
		cw.Write([]string{
			p.ID, p.Name, p.Gender,
			strconv.FormatFloat(p.GenderProbability, 'f', 4, 64),
			strconv.Itoa(p.Age), p.AgeGroup, p.CountryID, p.CountryName,
			strconv.FormatFloat(p.CountryProbability, 'f', 4, 64),
			p.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	cw.Flush()
}

// --- helpers ---

func respondPaginated(w http.ResponseWriter, r *http.Request, filters map[string]string, basePath string) {
	params := r.URL.Query()

	page, _ := strconv.Atoi(params.Get("page"))
	if page < 1 {
		page = 1
	}
	limit, _ := strconv.Atoi(params.Get("limit"))
	if limit < 1 || limit > 50 {
		limit = 10
	}

	sortBy, order := sortParams(params)
	sqlBase, args := buildQuery(filters)

	var total int
	db.DB.QueryRow("SELECT COUNT(*) "+sqlBase, args...).Scan(&total)

	finalQuery := fmt.Sprintf("SELECT id, name, gender, gender_probability, age, age_group, country_id, country_name, country_probability, created_at %s ORDER BY %s %s LIMIT %d OFFSET %d",
		sqlBase, sortBy, order, limit, (page-1)*limit)
	rows, err := db.DB.Query(finalQuery, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server failure")
		return
	}
	defer rows.Close()

	results := []models.Profile{}
	for rows.Next() {
		var p models.Profile
		rows.Scan(&p.ID, &p.Name, &p.Gender, &p.GenderProbability, &p.Age, &p.AgeGroup,
			&p.CountryID, &p.CountryName, &p.CountryProbability, &p.CreatedAt)
		results = append(results, p)
	}

	totalPages := (total + limit - 1) / limit

	self := buildPageURL(basePath, r, page, limit)
	var next, prev *string
	if page < totalPages {
		u := buildPageURL(basePath, r, page+1, limit)
		next = &u
	}
	if page > 1 {
		u := buildPageURL(basePath, r, page-1, limit)
		prev = &u
	}

	writeJSON(w, models.PaginatedResponse{
		Status:     "success",
		Page:       page,
		Limit:      limit,
		Total:      total,
		TotalPages: totalPages,
		Links:      models.PageLinks{Self: self, Next: next, Prev: prev},
		Data:       results,
	})
}

func buildPageURL(basePath string, r *http.Request, page, limit int) string {
	q := r.URL.Query()
	q.Set("page", strconv.Itoa(page))
	q.Set("limit", strconv.Itoa(limit))
	return basePath + "?" + q.Encode()
}

func sortParams(params interface{ Get(string) string }) (string, string) {
	sortBy := params.Get("sort_by")
	if sortBy != "age" && sortBy != "gender_probability" && sortBy != "created_at" {
		sortBy = "created_at"
	}
	order := strings.ToLower(params.Get("order"))
	if order != "asc" {
		order = "desc"
	}
	return sortBy, order
}

func extractFilters(params interface{ Get(string) string }, keys ...string) map[string]string {
	filters := make(map[string]string)
	for _, k := range keys {
		if v := params.Get(k); v != "" {
			filters[k] = v
		}
	}
	return filters
}
