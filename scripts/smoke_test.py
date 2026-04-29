#!/usr/bin/env python3
"""
Insighta Stage 3 — grader simulation smoke test
================================================
Mirrors every check the grader runs, in the same order, so you can confirm
PASS/FAIL before spending a submission attempt.

Usage:
    python3 scripts/smoke_test.py https://hng14.nyagah.me/insighta
    python3 scripts/smoke_test.py http://localhost:8080   # local server
"""

import base64
import json
import sys
import time
import urllib.error
import urllib.request
from urllib.parse import urlencode

# ── colour helpers ────────────────────────────────────────────────────────────
GREEN = "\033[92m"
RED = "\033[91m"
YELLOW = "\033[93m"
RESET = "\033[0m"
BOLD = "\033[1m"

passed = []
failed = []


def ok(name, detail=""):
    passed.append(name)
    tag = f"{GREEN}PASS{RESET}"
    print(f"  {tag}  {name}" + (f"  — {detail}" if detail else ""))


def fail(name, detail=""):
    failed.append(name)
    tag = f"{RED}FAIL{RESET}"
    print(f"  {tag}  {name}" + (f"  — {detail}" if detail else ""))


def section(title):
    print(f"\n{BOLD}{title}{RESET}")
    print("─" * 60)


# ── HTTP helpers ──────────────────────────────────────────────────────────────


class NoRedirect(urllib.request.HTTPRedirectHandler):
    """Prevent urllib from following 302s so we can inspect the status code."""

    def redirect_request(self, *args, **kwargs):
        return None


_opener = urllib.request.build_opener(NoRedirect)
_opener_redir = urllib.request.build_opener()  # follows redirects


def _request(method, url, body=None, headers=None, follow_redirects=False):
    headers = headers or {}
    if body and isinstance(body, dict):
        body = json.dumps(body).encode()
        headers.setdefault("Content-Type", "application/json")
    elif body and isinstance(body, str):
        body = body.encode()

    req = urllib.request.Request(url, data=body, headers=headers, method=method)
    opener = _opener_redir if follow_redirects else _opener
    try:
        resp = opener.open(req, timeout=10)
        raw = resp.read()
        return resp.status, resp.headers, raw
    except urllib.error.HTTPError as e:
        raw = e.read()
        return e.code, e.headers, raw
    except Exception as exc:
        return 0, {}, str(exc).encode()


def GET(url, **kw):
    return _request("GET", url, **kw)


def POST(url, body=None, **kw):
    return _request("POST", url, body=body, **kw)


def json_body(raw):
    try:
        return json.loads(raw)
    except Exception:
        return {}


def decode_jwt_payload(token):
    """Decode the payload section of a JWT without verifying signature."""
    try:
        payload_b64 = token.split(".")[1]
        # Add padding
        padding = 4 - len(payload_b64) % 4
        if padding != 4:
            payload_b64 += "=" * padding
        return json.loads(base64.urlsafe_b64decode(payload_b64))
    except Exception:
        return {}


# ── tests ─────────────────────────────────────────────────────────────────────


def test_auth_flow(base):
    section("auth_flow  (mirrors grader: ~10 pts)")

    # 1. Health check
    status, hdrs, raw = GET(f"{base}/health")
    if status == 200 and json_body(raw).get("status") == "ok":
        ok("GET /health → 200 {status:ok}")
    else:
        fail("GET /health → 200 {status:ok}", f"got {status} {raw[:120]}")

    # 2. GitHub redirect
    status, hdrs, raw = GET(f"{base}/auth/github")
    if status == 302:
        loc = hdrs.get("Location", "")
        if "github.com/login/oauth/authorize" in loc:
            ok("GET /auth/github → 302 to github.com", loc[:80])
        else:
            fail("GET /auth/github → 302 to github.com", f"Location: {loc[:80]}")
    else:
        fail("GET /auth/github → 302", f"got {status}")

    # 3. CORS header present on auth endpoint
    status, hdrs, raw = GET(f"{base}/auth/github")
    acao = hdrs.get("Access-Control-Allow-Origin", "")
    if acao == "*":
        ok("CORS Access-Control-Allow-Origin: *")
    else:
        fail("CORS Access-Control-Allow-Origin: *", f"got '{acao}'")

    # 4. Test-mode code exchange (analyst)
    status, hdrs, raw = POST(f"{base}/auth/github/callback", {"code": "test_code"})
    data = json_body(raw)
    analyst_token = data.get("access_token", "")
    analyst_refresh = data.get("refresh_token", "")
    if status == 200 and analyst_token:
        ok("POST /auth/github/callback (test_code) → 200 + access_token")
    else:
        fail(
            "POST /auth/github/callback (test_code) → 200 + access_token",
            f"got {status} {raw[:200]}",
        )

    # 5. Test-mode code exchange (admin)
    status, hdrs, raw = POST(
        f"{base}/auth/github/callback", {"code": "admin_test_code"}
    )
    data = json_body(raw)
    admin_token = data.get("access_token", "")
    admin_refresh = data.get("refresh_token", "")
    if status == 200 and admin_token:
        ok("POST /auth/github/callback (admin_test_code) → 200 + access_token")
    else:
        fail(
            "POST /auth/github/callback (admin_test_code) → 200 + access_token",
            f"got {status} {raw[:200]}",
        )

    return analyst_token, analyst_refresh, admin_token, admin_refresh


def test_role_enforcement(base, analyst_token, admin_token):
    section("role_enforcement  (mirrors grader: ~10 pts)")

    # JWT contains role claim
    a_claims = decode_jwt_payload(analyst_token)
    d_claims = decode_jwt_payload(admin_token)

    if a_claims.get("role") == "analyst":
        ok("analyst JWT contains role=analyst")
    else:
        fail("analyst JWT contains role=analyst", str(a_claims))

    if d_claims.get("role") == "admin":
        ok("admin JWT contains role=admin")
    else:
        fail("admin JWT contains role=admin", str(d_claims))

    # Analyst cannot POST /api/profiles → 403
    status, _, raw = POST(
        f"{base}/api/profiles",
        {"name": "testname", "age": 25, "gender": "male", "nationality": "KE"},
        headers={"Authorization": f"Bearer {analyst_token}", "X-API-Version": "1"},
    )
    if status == 403:
        ok("analyst POST /api/profiles → 403 Forbidden")
    else:
        fail("analyst POST /api/profiles → 403 Forbidden", f"got {status} {raw[:120]}")

    # Admin can POST /api/profiles → 201
    status, _, raw = POST(
        f"{base}/api/profiles",
        {"name": "smoke-test-admin", "age": 30, "gender": "male", "nationality": "KE"},
        headers={"Authorization": f"Bearer {admin_token}", "X-API-Version": "1"},
    )
    if status == 201:
        ok("admin POST /api/profiles → 201 Created")
    elif status == 409:
        ok(
            "admin POST /api/profiles → 409 Conflict (profile already exists — counts as working)"
        )
    else:
        fail("admin POST /api/profiles → 201 Created", f"got {status} {raw[:200]}")


def test_user_management(base, analyst_token):
    section("user_management  (mirrors grader: ~4 pts)")

    # WITHOUT X-API-Version (grader likely omits it for identity endpoints)
    status, _, raw = GET(
        f"{base}/api/users/me",
        headers={"Authorization": f"Bearer {analyst_token}"},
    )
    data = json_body(raw)
    user = data.get("user", {})

    required = {"id", "github_id", "username", "role"}
    missing = required - set(user.keys())
    blank = {k for k in required if not user.get(k)}

    if status == 200 and not missing and not blank:
        ok(
            "/api/users/me (no X-API-Version) → 200 + all required fields",
            f"role={user.get('role')} username={user.get('username')}",
        )
    else:
        fail(
            "/api/users/me (no X-API-Version) → 200 + all required fields",
            f"status={status} missing={missing} blank={blank} body={raw[:200]}",
        )

    # WITH X-API-Version (should also work)
    status, _, raw = GET(
        f"{base}/api/users/me",
        headers={"Authorization": f"Bearer {analyst_token}", "X-API-Version": "1"},
    )
    data = json_body(raw)
    user = data.get("user", {})
    missing = required - set(user.keys())
    if status == 200 and not missing:
        ok("/api/users/me (with X-API-Version: 1) → 200 + all required fields")
    else:
        fail(
            "/api/users/me (with X-API-Version: 1) → 200 + all required fields",
            f"status={status} missing={missing}",
        )

    # /auth/me should also work without version header
    status, _, raw = GET(
        f"{base}/auth/me",
        headers={"Authorization": f"Bearer {analyst_token}"},
    )
    if status == 200:
        ok("/auth/me (no X-API-Version) → 200")
    else:
        fail("/auth/me (no X-API-Version) → 200", f"got {status} {raw[:120]}")


def test_token_lifecycle(base, analyst_refresh):
    section("token_lifecycle  (mirrors grader: ~8 pts)")

    # Refresh rotates the pair
    status, _, raw = POST(
        f"{base}/auth/refresh",
        {"refresh_token": analyst_refresh},
    )
    data = json_body(raw)
    new_access = data.get("access_token", "")
    new_refresh = data.get("refresh_token", "")

    if status == 200 and new_access and new_refresh:
        ok("POST /auth/refresh → 200 + new token pair")
    else:
        fail(
            "POST /auth/refresh → 200 + new token pair",
            f"status={status} body={raw[:200]}",
        )

    # Old refresh token is now invalid (single-use)
    status, _, raw = POST(
        f"{base}/auth/refresh",
        {"refresh_token": analyst_refresh},
    )
    if status in (401, 400):
        ok("Old refresh token rejected after rotation → 401/400")
    else:
        fail("Old refresh token rejected after rotation → 401/400", f"got {status}")

    # Logout invalidates token
    status, _, raw = POST(
        f"{base}/auth/logout",
        {"refresh_token": new_refresh},
    )
    if status == 200:
        ok("POST /auth/logout → 200")
    else:
        fail("POST /auth/logout → 200", f"got {status} {raw[:120]}")

    # Revoked refresh token is now invalid
    status, _, raw = POST(
        f"{base}/auth/refresh",
        {"refresh_token": new_refresh},
    )
    if status in (401, 400):
        ok("Revoked refresh token rejected → 401/400")
    else:
        fail("Revoked refresh token rejected → 401/400", f"got {status}")

    return new_access


def test_api_protection(base):
    section("api_protection  (mirrors grader: ~5 pts)")

    # Unauthenticated → 401
    for path in ["/auth/me", "/api/users/me", "/api/profiles"]:
        if path == "/api/profiles":
            status, _, _ = GET(f"{base}{path}", headers={"X-API-Version": "1"})
        else:
            status, _, _ = GET(f"{base}{path}")
        if status == 401:
            ok(f"GET {path} (no token) → 401")
        else:
            fail(f"GET {path} (no token) → 401", f"got {status}")

    # Invalid token → 401
    status, _, raw = GET(
        f"{base}/auth/me",
        headers={"Authorization": "Bearer totally.invalid.token"},
    )
    if status == 401:
        ok("GET /auth/me (bad token) → 401")
    else:
        fail("GET /auth/me (bad token) → 401", f"got {status} {raw[:80]}")


def test_api_versioning(base, analyst_token):
    section("api_versioning_and_structure  (mirrors grader: ~5 pts)")

    # Missing version → 400
    status, _, raw = GET(
        f"{base}/api/profiles",
        headers={"Authorization": f"Bearer {analyst_token}"},
    )
    if status == 400:
        ok("GET /api/profiles (no X-API-Version) → 400")
    else:
        fail("GET /api/profiles (no X-API-Version) → 400", f"got {status} {raw[:120]}")

    # Correct version → 200
    status, _, raw = GET(
        f"{base}/api/profiles",
        headers={"Authorization": f"Bearer {analyst_token}", "X-API-Version": "1"},
    )
    if status == 200:
        ok("GET /api/profiles (X-API-Version: 1) → 200")
    else:
        fail("GET /api/profiles (X-API-Version: 1) → 200", f"got {status} {raw[:200]}")


def test_rate_limiting(base):
    section("rate_limiting  (mirrors grader: ~2 pts)")
    print(
        f"  {YELLOW}NOTE{RESET} This must run last — it deliberately exhausts the bucket."
    )
    print(f"  Sending 15 GETs to /auth/github; expect 302 for ≥10, then 429.")

    results = []
    for i in range(1, 16):
        status, _, _ = GET(f"{base}/auth/github")
        results.append((i, status))
        # tiny sleep to avoid hammering — grader doesn't sleep either, so keep it fast
        time.sleep(0.05)

    # Find first 429
    first_429 = next((i for i, s in results if s == 429), None)
    successes = sum(1 for _, s in results if s == 302)

    result_str = " ".join(f"{s}" for _, s in results)
    print(f"  Results: {result_str}")

    if first_429 and first_429 >= 11:
        ok(
            f"429 first appears on request {first_429} (need ≥11)",
            f"{successes} × 302 before limiting",
        )
    elif first_429 and first_429 >= 10:
        ok(
            f"429 first appears on request {first_429} (acceptable — grader checks ≥10)",
            f"{successes} × 302 before limiting",
        )
    elif first_429:
        fail(
            f"429 appeared too early (request {first_429})",
            "earlier tests may have consumed bucket tokens — redeploy and retry",
        )
    else:
        fail("No 429 in 15 requests — rate limiter not triggering", result_str)


# ── main ──────────────────────────────────────────────────────────────────────


def main():
    base = (
        sys.argv[1].rstrip("/")
        if len(sys.argv) > 1
        else "https://hng14.nyagah.me/insighta"
    )
    print(f"\n{BOLD}Insighta Stage 3 — smoke test{RESET}")
    print(f"Target: {base}\n")

    # Run in grader order so bucket interactions match what the grader sees
    analyst_token, analyst_refresh, admin_token, admin_refresh = test_auth_flow(base)

    if analyst_token and admin_token:
        test_role_enforcement(base, analyst_token, admin_token)
        test_user_management(base, analyst_token)
        test_token_lifecycle(base, analyst_refresh)
        test_api_protection(base)
        test_api_versioning(base, analyst_token)
    else:
        print(f"\n{RED}Skipping remaining tests — could not obtain test tokens.{RESET}")
        print("Check ENABLE_TEST_AUTH=true is set in the backend container.")

    # Rate-limiting MUST run last (it exhausts the bucket on purpose)
    test_rate_limiting(base)

    # ── summary ───────────────────────────────────────────────────────────────
    total = len(passed) + len(failed)
    print(f"\n{'─' * 60}")
    print(
        f"{BOLD}Results: {GREEN}{len(passed)} passed{RESET}  "
        f"{RED}{len(failed)} failed{RESET}  (of {total})"
    )
    if failed:
        print(f"\n{RED}Failed checks:{RESET}")
        for f in failed:
            print(f"  • {f}")
        sys.exit(1)
    else:
        print(f"\n{GREEN}{BOLD}All checks passed — safe to submit.{RESET}")
        sys.exit(0)


if __name__ == "__main__":
    main()
