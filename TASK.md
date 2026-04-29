# Stage 3 Backend Task: Secure Access & Multi-Interface Integration

| Field    | Details                                                        |
|----------|----------------------------------------------------------------|
| Deadline | 4/30/2026 1:59am                                               |
| Task     | Stage 3 (Backend): Secure Access & Multi-Interface Integration |
| Points   | 100 (Pass mark: 75)                                            |

---

## Overview

> Full technical specification: [Technical Requirements Document](./Technical%20Requirements%20Document.md)

Take the Profile Intelligence System from Stage 2 and turn it into a platform that real users can log into, with roles, sessions, and two working interfaces.

**Stage 2 stays intact.** Filtering, sorting, pagination, natural language search — all of it. Breaking anything from Stage 2 counts against you.

---

## What You Are Building

- GitHub OAuth with PKCE, for both the CLI and the browser
- Access + refresh token management with short expiry windows
- Role-based access control — `admin` and `analyst`, enforced across every endpoint
- API versioning and an updated pagination shape
- CSV profile export
- A globally installable CLI tool that stores credentials at `~/.insighta/credentials.json`
- A web portal with HTTP-only cookies and CSRF protection
- Rate limiting and request logging

---

## Repositories

Three repositories are required. The CLI and web portal share the same backend — one source of truth across all interfaces.

| Repo           | Description                              |
|----------------|------------------------------------------|
| Backend         | Core API (extends Stage 2)              |
| CLI             | Globally installable CLI tool           |
| Web Portal      | Browser-based interface                 |

---

## Functional Requirements

### 1. Authentication — GitHub OAuth with PKCE

- Implement the full PKCE flow for both CLI and browser clients
- Issue short-lived **access tokens** and **refresh tokens**
- Handle token refresh transparently

### 2. Role-Based Access Control

| Role      | Description                              |
|-----------|------------------------------------------|
| `admin`   | Full access to all endpoints             |
| `analyst` | Read-only access to profile data         |

- Roles must be enforced on **every endpoint**
- Unauthorized access returns a proper `403` response

### 3. API Versioning

- Version the API (e.g. `/api/v1/profiles`)
- Update the pagination response shape as needed

### 4. CSV Export

- Add an endpoint to export filtered profile data as a CSV file

### 5. CLI Tool

- Globally installable (e.g. via `npm install -g` or a compiled binary)
- Stores credentials at `~/.insighta/credentials.json`
- Supports rich terminal output
- Handles token storage, refresh, and expiry gracefully

**Required commands (at minimum):**
- Login via GitHub OAuth (PKCE)
- Query/filter profiles
- Export profiles to CSV
- Logout / clear credentials

### 6. Web Portal

- Full login flow via GitHub OAuth
- Uses **HTTP-only cookies** for session management
- **CSRF protection** on all state-mutating requests
- Pages for browsing, filtering, and viewing profiles

### 7. Rate Limiting & Request Logging

- Apply rate limiting to all API endpoints
- Log all incoming requests (method, path, status, latency)

### 8. CI/CD

- CI/CD pipeline set up for at least the backend repository

---

## Error Responses

All errors follow this structure:

```json
{ "status": "error", "message": "<error message>" }
```

| Status Code | Meaning                     |
|-------------|-----------------------------|
| `400`       | Bad request / missing params |
| `401`       | Unauthenticated             |
| `403`       | Forbidden (wrong role)      |
| `404`       | Resource not found          |
| `422`       | Invalid parameter type      |
| `429`       | Rate limit exceeded         |
| `500`/`502` | Server failure              |

---

## Additional Requirements

- CORS header: `Access-Control-Allow-Origin: *` (except cookie-based portal endpoints)
- All timestamps in **UTC ISO 8601**
- All IDs in **UUID v7**

---

## Evaluation Criteria

| Criterion                                     | Points |
|-----------------------------------------------|--------|
| Authentication & PKCE flow                    | 20     |
| Role enforcement                              | 10     |
| CLI (commands + rich output + token handling) | 20     |
| Web portal (pages + HTTP-only cookies)        | 15     |
| API updates (versioning, pagination, export)  | 10     |
| Rate limiting & logging                       | 5      |
| CI/CD setup                                   | 5      |
| Engineering standards (commits, PRs, branches)| 5      |
| README completeness                           | 10     |
| **Total**                                     | **100**|

---

## README Requirements

Your README must cover:

- System Architecture
- Auth flow (PKCE, tokens, refresh)
- CLI usage (installation + all commands)
- Token handling approach
- Role enforcement logic
- Natural language parsing approach (carry-over from Stage 2)

---

## Submission Format

Submit via `/submit` in `#stage-3-backend`:

1. **Backend repository** (public)
2. **CLI repository** (public)
3. **Web portal repository** (public)
4. **Live backend URL**
5. **Live web portal URL**
