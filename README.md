# Insighta Backend — Stage 3

Secure, multi-interface Profile Intelligence API built in Go.  
Extends Stage 2 with GitHub OAuth, RBAC, token management, CSV export, and API versioning.

---

## System Architecture

```
┌─────────────┐     JWT / refresh token      ┌──────────────────┐
│   CLI tool  │ ──────────────────────────── │                  │
└─────────────┘                               │  insighta-backend│
                                              │  (this repo)     │
┌─────────────┐     HTTP-only cookie session  │                  │
│ Web portal  │ ──────────────────────────── │                  │
└─────────────┘                               └────────┬─────────┘
                                                       │
                                               SQLite (WAL mode)
```

Both the CLI and web portal share this single backend. It is the only source of truth.

---

## Running Locally

### Prerequisites

- Go 1.21+
- A GitHub OAuth App ([create one here](https://github.com/settings/developers))
  - Homepage URL: `http://localhost:8080`
  - Authorization callback URL: `http://localhost:8080/auth/github/callback`

### Setup

```bash
cp .env.example .env
# fill in .env — see Configuration section below
go run ./cmd/server
```

The SQLite database is created automatically on first run.

### Verify

```bash
curl http://localhost:8080/health
# {"status":"ok"}
```

---

## Configuration

| Variable | Required | Description |
|---|---|---|
| `PORT` | no | Server port (default: `8080`) |
| `DB_PATH` | no | SQLite file path (default: `./insighta.db`) |
| `GITHUB_CLIENT_ID` | **yes** | GitHub OAuth App client ID |
| `GITHUB_CLIENT_SECRET` | **yes** | GitHub OAuth App client secret |
| `GITHUB_REDIRECT_URI` | **yes** | Must match the callback URL registered in GitHub |
| `JWT_SECRET` | **yes** | HMAC signing secret — minimum 32 random characters |
| `GENDERIZE_API` | no | Override genderize.io base URL |
| `AGIFY_API` | no | Override agify.io base URL |
| `NATIONALIZE_API` | no | Override nationalize.io base URL |

Generate a secure JWT secret:
```bash
openssl rand -hex 32
```

---

## Authentication Flow

### Browser (Web Portal)

```
Browser                   Backend                    GitHub
  │── GET /auth/github ──────────────────────────────────│
  │                         │── redirect ──────────────> │
  │ <─────────────────────────────────── callback+code ──│
  │                         │── POST /login/oauth/access_token
  │                         │── GET /user (GitHub API)
  │                         │   upsert user row
  │ <── access_token + refresh_token ───────────────────│
```

### CLI (PKCE Flow)

1. CLI generates `state`, `code_verifier`, and `code_challenge` (S256)
2. CLI starts a local HTTP server on a random port
3. CLI opens `GET /auth/github?state=...&code_challenge=...` in the browser
4. GitHub redirects to the CLI's local server with `?code=...`
5. CLI posts `{ code, code_verifier }` to `POST /auth/github/callback`
6. Backend exchanges code with GitHub, upserts user, issues token pair
7. CLI stores tokens at `~/.insighta/credentials.json`

### Tokens

| Token | Type | Expiry | Storage |
|---|---|---|---|
| Access token | Signed JWT (HS256) | 3 minutes | Client memory / credentials file |
| Refresh token | Opaque random (SHA-256 hashed in DB) | 5 minutes | Client credentials file |

Refresh tokens are **single-use** — each `/auth/refresh` call issues a new pair and invalidates the old one.

---

## API Reference

### Auth Endpoints

All auth endpoints are rate-limited to **10 requests / minute per IP**.

| Method | Path | Description |
|---|---|---|
| `GET` | `/auth/github` | Redirect to GitHub OAuth |
| `GET` / `POST` | `/auth/github/callback` | Handle OAuth callback, issue tokens |
| `POST` | `/auth/refresh` | Rotate token pair |
| `POST` | `/auth/logout` | Invalidate refresh token |

#### `POST /auth/refresh`
```json
// request
{ "refresh_token": "..." }

// response
{ "status": "success", "access_token": "...", "refresh_token": "..." }
```

#### `POST /auth/logout`
```json
// request
{ "refresh_token": "..." }

// response
{ "status": "success", "message": "logged out" }
```

---

### Profile Endpoints

All profile endpoints require:
- `Authorization: Bearer <access_token>`
- `X-API-Version: 1`

Rate limit: **60 requests / minute per user**.

| Method | Path | Role | Description |
|---|---|---|---|
| `GET` | `/api/profiles` | analyst+ | List profiles with filters/sort/pagination |
| `GET` | `/api/profiles/search?q=` | analyst+ | Natural language search |
| `GET` | `/api/profiles/export?format=csv` | analyst+ | Download filtered profiles as CSV |
| `GET` | `/api/profiles/{id}` | analyst+ | Get single profile |
| `POST` | `/api/profiles` | **admin** | Create profile via external APIs |

#### Query Parameters (list + export)

| Param | Type | Description |
|---|---|---|
| `gender` | string | `male` or `female` |
| `age_group` | string | `child`, `teenager`, `adult`, `senior` |
| `country_id` | string | ISO 3166-1 alpha-2 code |
| `min_age` / `max_age` | int | Age range |
| `min_gender_probability` | float | Minimum confidence |
| `min_country_probability` | float | Minimum confidence |
| `sort_by` | string | `age`, `gender_probability`, `created_at` (default) |
| `order` | string | `asc` or `desc` (default) |
| `page` | int | Page number (default: 1) |
| `limit` | int | Results per page, max 50 (default: 10) |

#### Paginated Response Shape

```json
{
  "status": "success",
  "page": 1,
  "limit": 10,
  "total": 2026,
  "total_pages": 203,
  "links": {
    "self": "/api/profiles?page=1&limit=10",
    "next": "/api/profiles?page=2&limit=10",
    "prev": null
  },
  "data": [ ... ]
}
```

#### `POST /api/profiles` (admin only)
```json
// request
{ "name": "Harriet Tubman" }

// response — 201 Created
{
  "status": "success",
  "data": {
    "id": "uuid",
    "name": "Harriet Tubman",
    "gender": "female",
    "gender_probability": 0.97,
    "age": 28,
    "age_group": "adult",
    "country_id": "US",
    "country_name": "United States",
    "country_probability": 0.89,
    "created_at": "2026-04-28T00:00:00Z"
  }
}
```

---

## Role Enforcement

| Role | Permissions |
|---|---|
| `admin` | Full access — read, create, export |
| `analyst` | Read-only — list, search, get, export |

Default role for new users: **analyst**.

Roles are enforced in the `RequireRole` middleware applied at the router level — no scattered per-handler checks.

Disabled users (`is_active = false`) receive `403 Forbidden` on all requests regardless of role.

---

## Natural Language Search

`GET /api/profiles/search?q=young+males+from+nigeria`

The rule-based parser recognises:

| Phrase | Maps to |
|---|---|
| `male` / `female` | `gender` |
| `young` | `min_age=16`, `max_age=24` |
| `teenager` / `adult` / `senior` / `child` | `age_group` |
| `above N` | `min_age=N+1` |
| `under N` | `max_age=N-1` |
| Country names (nigeria, kenya, ghana, etc.) | `country_id` |

---

## Error Responses

All errors follow a consistent shape:

```json
{ "status": "error", "message": "<description>" }
```

| Status | Meaning |
|---|---|
| `400` | Bad request / missing params / missing API version header |
| `401` | Unauthenticated or expired token |
| `403` | Forbidden (wrong role or disabled account) |
| `404` | Resource not found |
| `409` | Conflict (duplicate profile name) |
| `429` | Rate limit exceeded |
| `500` / `502` | Server / upstream failure |

---

## Project Structure

```
backend/stage-3/
├── cmd/server/main.go          — entry point, chi router, route wiring
├── internal/
│   ├── models/models.go        — shared types: Profile, User, RefreshToken, responses
│   ├── db/db.go                — SQLite init (WAL mode) + schema migration
│   ├── auth/
│   │   ├── pkce.go             — S256Challenge, GenerateState, HashToken
│   │   ├── tokens.go           — JWT issuance, refresh token rotation + revocation
│   │   └── handler.go          — OAuth endpoints
│   ├── middleware/
│   │   ├── logger.go           — request logger (method/path/status/latency)
│   │   ├── ratelimit.go        — token-bucket rate limiter
│   │   ├── auth.go             — JWT validation, user context injection, RBAC
│   │   └── version.go          — X-API-Version header enforcement
│   └── profiles/
│       ├── service.go          — CreateProfile (genderize / agify / nationalize)
│       ├── handler.go          — List, Search, Get, Create, Export handlers
│       └── json.go             — encode/decode helpers
├── Dockerfile
├── .env.example
└── go.mod
```

---

## Docker

```bash
docker build -t insighta-backend .
docker run -p 8080:8080 --env-file .env -v $(pwd)/data:/app/data \
  -e DB_PATH=/app/data/insighta.db insighta-backend
```

---

## Deployment (VPS)

See the root `docker-compose.prod.yml` for the full stack configuration with Traefik.
