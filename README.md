# insighta-backend (short)

Lightweight Go backend for Insighta: GitHub OAuth, short-lived JWTs, rotating refresh tokens, SQLite storage.

## Quick tech
- Go 1.26, chi router
- SQLite (go-sqlite3), WAL mode
- JWT (HS256) + opaque refresh tokens

## Quick start (local)
1. Create `.env` in repo root with required vars (example below):
```/dev/null/.env#L1-8
PORT=8080
DB_PATH=./insighta.db
GITHUB_CLIENT_ID=your_github_client_id
GITHUB_CLIENT_SECRET=your_github_client_secret
GITHUB_REDIRECT_URI=http://localhost:8080/auth/github/callback
JWT_SECRET=a_long_random_secret_string
```
2. Run:
```/dev/null/shell.sh#L1-3
go mod download
go run ./cmd/server
```
Server listens on `:8080`.

Note: `go-sqlite3` requires CGO (install `gcc`) to build.

## Docker Compose
The `docker-compose.yml` now reads `.env` from the repo root by default. To run with Compose:

```/dev/null/shell.sh#L1-3
# build and start
docker compose up --build

# stop and remove
docker compose down
```

If you prefer passing an env file explicitly you can still do:
```/dev/null/shell.sh#L1-2
docker compose --env-file .env up --build
```

The DB is persisted in a Docker volume `backend-data` mounted at `/app/data`.

## Important env vars
- `GITHUB_CLIENT_ID`, `GITHUB_CLIENT_SECRET`, `GITHUB_REDIRECT_URI` — GitHub OAuth
- `JWT_SECRET` — JWT signing secret (required)
- `DB_PATH` — SQLite file path (default `./insighta.db`)

## Routes (summary)
- GET /health — liveness

Auth (unauthenticated group)
- GET /auth/github — redirect to GitHub (supports PKCE query params)
- GET|POST /auth/github/callback — exchange code; returns {access_token, refresh_token, user}
- POST /auth/refresh — rotate tokens (body {refresh_token})
- POST /auth/logout — revoke refresh token

Authenticated API (requires `Authorization: Bearer <jwt>` and header `X-API-Version: 1`)
- GET /auth/me — current user

Profiles (authenticated)
- GET /api/profiles — list (filters, pagination)
- GET /api/profiles/search?q=... — simple NLP-backed search
- GET /api/profiles/{id} — single profile
- GET /api/profiles/export?format=csv — CSV export
- POST /api/profiles — create (admin only)

## Auth details
- Access token (JWT) lifetime: 3 minutes
- Refresh token lifetime: 5 minutes (single-use; rotate via `/auth/refresh`)
- New users auto-created with role `analyst`. Promote to `admin` via DB.

## Rate limiting
- Auth routes: 10 req/min per IP
- Authenticated API: 60 req/min per user

## Promotions / Admin
Promote a user by updating the SQLite `users` table:
```/dev/null/sql.sql#L1
UPDATE users SET role = 'admin' WHERE username = 'their_github_login';
```

## Project layout (brief)
- `cmd/server/main.go` — entry, router
- `internal/auth` — OAuth handlers, token logic
- `internal/profiles` — profile endpoints and logic
- `internal/db` — sqlite init / migrations
- `internal/middleware` — auth, rate limit, version checks
- `internal/models` — shared types

If you want this even shorter (one-page cheatsheet) or want me to change compose to read `.env` in-place, tell me which and I'll update. 