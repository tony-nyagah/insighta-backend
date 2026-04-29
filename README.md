# insighta-backend

The REST API server for [Insighta](https://insighta.app) — a profile intelligence platform built on top of GitHub OAuth, short-lived JWTs, and rotating refresh tokens backed by SQLite.

---

## Table of Contents

1. [System Architecture](#system-architecture)
2. [Quick Start](#quick-start)
3. [Configuration](#configuration)
4. [Authentication Flow](#authentication-flow)
5. [Token Handling](#token-handling)
6. [CLI Usage](#cli-usage)
7. [API Routes](#api-routes)
8. [Role Enforcement](#role-enforcement)
9. [Natural Language Search](#natural-language-search)
10. [Rate Limiting](#rate-limiting)
11. [API Versioning](#api-versioning)
12. [Response Shapes](#response-shapes)
13. [Docker](#docker)
14. [Promoting a User to Admin](#promoting-a-user-to-admin)
15. [Project Structure](#project-structure)

---

## System Architecture

Insighta is made up of three services. This repo is the backend.

```
┌──────────────────────┐                   ┌─────────────────────────┐
│   insighta-cli       │  REST + JWT        │                         │
│   (Cobra CLI tool)   │ ──────────────────>│   insighta-backend      │
└──────────────────────┘                   │   Go 1.26 · chi router  │
                                           │   :8080                  │
┌──────────────────────┐                   │                         │
│   insighta-web       │  REST + JWT        │                         │
│   (Go + HTMX portal) │ ──────────────────>│                         │
│   :8069              │                   └────────────┬────────────┘
└──────────────────────┘                                │
                                               SQLite (WAL mode)
                                           persisted in Docker volume
```

**Key design decisions:**

- **Single datastore**: SQLite in WAL mode is the only storage layer. No Redis, no Postgres. The database file is persisted in a named Docker volume so it survives container restarts.
- **Single backend**: both the web portal and the CLI authenticate against this one service over HTTP. Neither client holds privileged knowledge — everything is enforced here.
- **Identifiers**: all IDs are UUID v7 (time-ordered). All timestamps are UTC ISO 8601.
- **Dependencies**: [chi](https://github.com/go-chi/chi) for routing, [go-sqlite3](https://github.com/mattn/go-sqlite3) (CGO) for SQLite, `gorilla/sessions` and `gorilla/securecookie` in the web portal layer.

---

## Quick Start

> **Prerequisite**: `gcc` must be on your `PATH` because `go-sqlite3` uses CGO.

```insighta/insighta-backend/.env.example#L1-6
cp .env.example .env   # fill in the required values (see Configuration)
go run ./cmd/server
```

The server starts on `:8080` by default and creates `insighta.db` automatically on first run.

Verify it is up:

```insighta/insighta-backend/README.md#L1-1
curl http://localhost:8080/health
```

Expected response: `{"status":"ok"}`

---

## Configuration

| Variable | Required | Default | Description |
|---|---|---|---|
| `GITHUB_CLIENT_ID` | **yes** | — | GitHub OAuth App client ID |
| `GITHUB_CLIENT_SECRET` | **yes** | — | GitHub OAuth App client secret |
| `GITHUB_REDIRECT_URI` | **yes** | — | Must match the callback URL registered in your GitHub OAuth App |
| `JWT_SECRET` | **yes** | — | HMAC-SHA256 signing secret — use at least 32 random bytes |
| `DB_PATH` | no | `./insighta.db` | Path to the SQLite database file |
| `PORT` | no | `8080` | Port the server listens on |

Generate a secure `JWT_SECRET`:

```insighta/insighta-backend/README.md#L1-1
openssl rand -hex 32
```

**GitHub OAuth App settings** (create at <https://github.com/settings/developers>):

| Field | Value (local dev) |
|---|---|
| Homepage URL | `http://localhost:8080` |
| Authorization callback URL | `http://localhost:8080/auth/github/callback` |

---

## Authentication Flow

There are two distinct flows depending on the client.

### PKCE Flow (CLI)

The CLI never receives a raw GitHub redirect in the browser — it proves it initiated the request by presenting the original code verifier.

```
CLI                          Backend                       GitHub
 │                               │                            │
 │ 1. generate state,            │                            │
 │    code_verifier,             │                            │
 │    code_challenge             │                            │
 │    (S256)                     │                            │
 │                               │                            │
 │ 2. open browser to            │                            │
 │    GET /auth/github?          │                            │
 │      state=X                  │                            │
 │      &code_challenge=Y        │                            │
 │      &code_challenge_method=S256                           │
 │      &redirect_uri=           │                            │
 │        http://127.0.0.1:PORT/callback                      │
 │                               │                            │
 │                3. store       │                            │
 │                   state→challenge                          │
 │                   (10 min TTL)│                            │
 │                               │                            │
 │                               │── redirect to GitHub ───> │
 │                               │                            │
 │ <─────────────── http://127.0.0.1:PORT/callback            │
 │                                ?code=Z&state=X ───────────│
 │                               │                            │
 │ 4. validate state             │                            │
 │                               │                            │
 │ 5. POST /auth/github/callback │                            │
 │    { code: Z,                 │                            │
 │      code_verifier: ...,      │                            │
 │      state: X }               │                            │
 │                               │                            │
 │              6. retrieve challenge by state                │
 │                 assert BASE64URL(SHA256(code_verifier))    │
 │                   == stored challenge                      │
 │                               │                            │
 │                               │── exchange code ─────────>│
 │                               │<─ GitHub user ────────────│
 │                               │   upsert user row         │
 │ <─── { access_token,          │                            │
 │        refresh_token,         │                            │
 │        user }                 │                            │
 │                               │                            │
 │ 7. write ~/.insighta/         │                            │
 │    credentials.json           │                            │
```

**PKCE construction** (step 1):

- `state`: cryptographically random string
- `code_verifier`: cryptographically random string (43–128 chars, unreserved ASCII)
- `code_challenge`: `BASE64URL-no-padding(SHA256(code_verifier))`
- `code_challenge_method`: `S256`

### Browser Flow (Web Portal)

The web portal's server-side Go handler manages the OAuth round-trip itself.

1. User clicks "Login with GitHub" → portal redirects to `GET /auth/github` (no PKCE params)
2. GitHub redirects to the backend callback with `?code=Z&state=X`
3. Portal's server sends `POST /auth/github/callback { "code": "Z" }` to the backend
4. Backend exchanges the code with GitHub, upserts the user, returns the token pair
5. Portal stores the tokens in an **encrypted, HTTP-only session cookie** — never in `localStorage`

---

## Token Handling

### Access Token

- **Format**: signed JWT, algorithm HS256
- **Expiry**: 3 minutes
- **Claims**: `user_id`, `username`, `role`, standard `exp`/`iat`
- **Transport**: `Authorization: Bearer <token>` header

### Refresh Token

- **Format**: opaque random string (not a JWT)
- **Storage**: SHA-256 hashed before being written to the database — the raw token is never persisted
- **Expiry**: 5 minutes
- **Rotation**: single-use. When `/auth/refresh` is called, the current token's `used_at` column is set and a fresh pair is issued. Reusing an already-used refresh token returns `401`.

### Auto-Refresh Behaviour

| Situation | CLI behaviour | Web portal behaviour |
|---|---|---|
| Token within 10 s of expiry | Refreshes proactively before the next request | Same |
| Request returns `401` | One automatic retry after refresh | Same |
| Refresh token is also expired/invalid | Prompts user to log in again | Redirects to `/auth/github` |

Clients retry on `429 Too Many Requests` with exponential backoff: 500 ms → 1 s → 2 s, up to 3 attempts.

---

## CLI Usage

The CLI binary is `insighta`. It targets `https://api.insighta.app` by default. Override with the `API_URL` environment variable:

```insighta/insighta-backend/README.md#L1-1
API_URL=http://localhost:8080 insighta profiles list
```

Credentials are stored at `~/.insighta/credentials.json` after a successful login.

### Authentication

```insighta/insighta-backend/README.md#L1-6
# Open a browser window, complete GitHub OAuth, store credentials locally
insighta login

# Revoke the stored refresh token and delete credentials
insighta logout

# Print the currently authenticated user
insighta whoami
```

### Profiles

#### List

```insighta/insighta-backend/README.md#L1-13
insighta profiles list \
  [--gender male|female] \
  [--country <ISO-3166-alpha-2>] \
  [--age-group adult|teenager|senior|child] \
  [--min-age <N>] \
  [--max-age <N>] \
  [--sort-by age|created_at] \
  [--order asc|desc] \
  [--page <N>] \
  [--limit <N>]
```

All flags are optional. Results are paginated; default page size is 10.

#### Get a single profile

```insighta/insighta-backend/README.md#L1-1
insighta profiles get <id>
```

#### Natural language search

```insighta/insighta-backend/README.md#L1-3
# The query is parsed into structured filters server-side (see Natural Language Search)
insighta profiles search "young males from nigeria"
insighta profiles search "adult females under 35 from kenya"
```

#### Create (admin only)

```insighta/insighta-backend/README.md#L1-2
# Gender, age, and country are inferred from the name via external APIs
insighta profiles create --name "<full name>"
```

Returns `403 Forbidden` if the authenticated user does not have the `admin` role.

#### Export

```insighta/insighta-backend/README.md#L1-7
insighta profiles export \
  --format csv \
  [--gender male|female] \
  [--country <ISO-3166-alpha-2>] \
  [--age-group adult|teenager|senior|child]
```

Writes the CSV file to stdout. Redirect to a file as needed:

```insighta/insighta-backend/README.md#L1-1
insighta profiles export --format csv --country NG > nigeria_profiles.csv
```

---

## API Routes

### Public

| Method | Path | Description |
|---|---|---|
| `GET` | `/health` | Liveness check — returns `{"status":"ok"}` |
| `GET` | `/auth/github` | Initiate OAuth (supports optional PKCE query params) |
| `GET` | `/auth/github/callback` | OAuth redirect target (browser flow) |
| `POST` | `/auth/github/callback` | Code exchange (CLI / portal server-to-server) |
| `POST` | `/auth/refresh` | Rotate token pair |
| `POST` | `/auth/logout` | Revoke refresh token |

Auth routes are rate-limited to **10 requests / minute per IP**.

### Authenticated

All routes below require:

- `Authorization: Bearer <access_token>`
- `X-API-Version: 1`

Rate limit: **60 requests / minute per user ID**.

| Method | Path | Role | Description |
|---|---|---|---|
| `GET` | `/auth/me` | any | Current authenticated user |
| `GET` | `/api/profiles` | analyst+ | List profiles with filters/sort/pagination |
| `GET` | `/api/profiles/search?q=` | analyst+ | Natural language profile search |
| `GET` | `/api/profiles/export?format=csv` | analyst+ | Download profiles as CSV |
| `GET` | `/api/profiles/{id}` | analyst+ | Fetch a single profile by UUID |
| `POST` | `/api/profiles` | **admin** | Create a new profile |

#### `POST /auth/refresh`

```insighta/insighta-backend/README.md#L1-9
// Request
{ "refresh_token": "<opaque token>" }

// Response 200
{
  "status": "success",
  "access_token": "<new jwt>",
  "refresh_token": "<new opaque token>"
}
```

#### `POST /auth/logout`

```insighta/insighta-backend/README.md#L1-7
// Request
{ "refresh_token": "<opaque token>" }

// Response 200
{ "status": "success", "message": "logged out" }
```

#### `POST /api/profiles` (admin only)

```insighta/insighta-backend/README.md#L1-20
// Request
{ "name": "Harriet Tubman" }

// Response 201
{
  "status": "success",
  "data": {
    "id": "019500ab-...",
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

#### Query Parameters (list + search + export)

| Parameter | Type | Description |
|---|---|---|
| `gender` | string | `male` or `female` |
| `age_group` | string | `child`, `teenager`, `adult`, or `senior` |
| `country_id` | string | ISO 3166-1 alpha-2 code (e.g. `NG`, `KE`) |
| `min_age` | int | Lower bound on age (inclusive) |
| `max_age` | int | Upper bound on age (inclusive) |
| `sort_by` | string | `age` or `created_at` (default: `created_at`) |
| `order` | string | `asc` or `desc` (default: `desc`) |
| `page` | int | Page number, 1-based (default: `1`) |
| `limit` | int | Results per page, max 50 (default: `10`) |

---

## Role Enforcement

### Roles

| Role | Permissions |
|---|---|
| `admin` | Full access — list, get, search, export, create |
| `analyst` | Read-only — list, get, search, export |

New users are assigned the `analyst` role automatically at registration.

### Implementation

Role checks are applied at the **router level** — not inside individual handlers. The middleware stack for a protected route looks like this:

```insighta/insighta-backend/README.md#L1-5
Authenticate        → validates JWT signature + expiry,
                      loads a fresh user row from the DB,
                      injects user into request context,
                      returns 403 if is_active = false

RequireRole("admin") → reads role from context,
                       returns 403 if role != admin
```

`Authenticate` always runs first. A user with a valid JWT who has been deactivated (`is_active = false`) is blocked with `403 Forbidden` before any role check takes place.

`RequireRole` implements a simple hierarchy: `admin` satisfies any role requirement; `analyst` only satisfies `analyst`-level checks.

---

## Natural Language Search

`GET /api/profiles/search?q=young+males+from+nigeria`

The parser lowercases the query string and applies rule-based pattern matching — no external NLP library.

### Supported Patterns

| Input phrase | Extracted filter |
|---|---|
| `male` / `female` | `gender=male` / `gender=female` |
| `young` | `min_age=16`, `max_age=24` |
| `teenager` | `age_group=teenager` |
| `adult` | `age_group=adult` |
| `senior` | `age_group=senior` |
| `child` | `age_group=child` |
| `above N` | `min_age=N+1` |
| `under N` | `max_age=N-1` |
| Country name (e.g. `nigeria`, `kenya`, `ghana`) | `country_id=NG` / `KE` / `GH` |

Country names are mapped to ISO 3166-1 alpha-2 codes via a static keyword table.

If the parser cannot extract **at least one filter** from the query, it returns `400 Bad Request` rather than returning an unfiltered full-table result.

**Examples:**

```insighta/insighta-backend/README.md#L1-6
GET /api/profiles/search?q=young+females+from+kenya
→ gender=female, min_age=16, max_age=24, country_id=KE

GET /api/profiles/search?q=adult+males+above+30
→ gender=male, age_group=adult, min_age=31

GET /api/profiles/search?q=senior+women
→ gender=female, age_group=senior
```

---

## Rate Limiting

The rate limiter uses a **token-bucket** algorithm.

| Scope | Limit | Key |
|---|---|---|
| Auth routes (`/auth/*`) | 10 req / min | IP address |
| Authenticated API (`/api/*`) | 60 req / min | User ID (from JWT) |

When the limit is exceeded:

```insighta/insighta-backend/README.md#L1-4
HTTP/1.1 429 Too Many Requests

{ "status": "error", "message": "rate limit exceeded" }
```

CLI and web clients retry on `429` using exponential backoff: **500 ms → 1 s → 2 s**, maximum 3 retries, then surface the error to the user.

---

## API Versioning

Every request to `/api/*` must include the header:

```insighta/insighta-backend/README.md#L1-1
X-API-Version: 1
```

A missing or unrecognised version header returns `400 Bad Request` before any handler logic runs.

---

## Response Shapes

### Success — single resource

```insighta/insighta-backend/README.md#L1-4
{
  "status": "success",
  "data": { ... }
}
```

### Success — paginated list

```insighta/insighta-backend/README.md#L1-14
{
  "status": "success",
  "page": 1,
  "limit": 10,
  "total": 2026,
  "total_pages": 203,
  "links": {
    "self":  "/api/profiles?page=1&limit=10",
    "next":  "/api/profiles?page=2&limit=10",
    "prev":  null
  },
  "data": [ ... ]
}
```

### Error

```insighta/insighta-backend/README.md#L1-4
{
  "status": "error",
  "message": "<human-readable description>"
}
```

| Status code | Meaning |
|---|---|
| `400` | Bad request — malformed body, missing required field, missing API version header, or unparseable NL query |
| `401` | Unauthenticated — missing, expired, or invalid JWT |
| `403` | Forbidden — insufficient role or deactivated account |
| `404` | Resource not found |
| `429` | Rate limit exceeded |
| `500` | Internal server error |
| `502` | Bad gateway — upstream (GitHub API) failure |

---

## Docker

A `docker-compose.yml` is included at the repo root. The database is persisted in the named volume `backend-data` mounted at `/app/data` inside the container.

```insighta/insighta-backend/README.md#L1-2
# Build image and start the backend
docker compose up --build
```

```insighta/insighta-backend/README.md#L1-2
# Stop and remove containers (volume is preserved)
docker compose down
```

Compose reads `.env` from the repo root automatically. To pass a different env file:

```insighta/insighta-backend/README.md#L1-1
docker compose --env-file /path/to/other.env up --build
```

To run standalone (without Compose):

```insighta/insighta-backend/README.md#L1-4
docker build -t insighta-backend .
docker run -p 8080:8080 \
  --env-file .env \
  -v insighta-data:/app/data \
  -e DB_PATH=/app/data/insighta.db \
  insighta-backend
```

---

## Promoting a User to Admin

The `role` column on the `users` table is the authoritative source. Use the SQLite CLI or any compatible tool:

```insighta/insighta-backend/README.md#L1-1
UPDATE users SET role = 'admin' WHERE username = 'their-github-login';
```

There is intentionally no API endpoint for role promotion — it is a deliberate out-of-band operation.

---

## Project Structure

```insighta/insighta-backend/README.md#L1-18
insighta-backend/
├── cmd/
│   └── server/
│       └── main.go              — entry point: router wiring, middleware chain, server startup
├── internal/
│   ├── auth/
│   │   ├── handler.go           — OAuth endpoints (initiate, callback, refresh, logout, me)
│   │   ├── pkce.go              — PKCE helpers: GenerateState, S256Challenge, HashToken
│   │   └── tokens.go            — JWT issuance, refresh token rotation and revocation
│   ├── db/
│   │   └── db.go                — SQLite init (WAL mode), schema migration
│   ├── middleware/
│   │   ├── auth.go              — Authenticate middleware, RequireRole factory
│   │   ├── logger.go            — request logger (method / path / status / latency)
│   │   ├── ratelimit.go         — token-bucket rate limiter (per-IP and per-user)
│   │   └── version.go           — X-API-Version header enforcement
│   ├── models/
│   │   └── models.go            — shared types: Profile, User, RefreshToken, response envelopes
│   └── profiles/
│       ├── handler.go           — List, Get, Search, Create, Export HTTP handlers
│       ├── nlp.go               — rule-based natural language query parser
│       └── service.go           — CreateProfile: calls genderize / agify / nationalize APIs
├── Dockerfile
├── docker-compose.yml
├── .env.example
└── go.mod
```
