# EazyRent API

House-rental backend for the Nigerian market. Go with stdlib `net/http` only — no web framework.

## Run

Needs Go 1.27+, Postgres with PostGIS, and `task`.

```powershell
Copy-Item .env.example .env   # set DATABASE_URL, ACCESS_TOKEN_SECRET, BREVO_API_KEY
task dev                       # migrate up, serve on :8080
```

Swagger UI: `http://localhost:8080/swagger/index.html`

## Endpoints

Auth: `POST /auth/signup`, `/auth/signin`, `/auth/refresh`, `/auth/signout`, `GET /auth/verify?token=`, `POST /auth/forgot-password`, `POST /auth/reset-password`

Users: `GET /me`, `PUT /me/avatar`

Listings: `GET /listings` (filters: `status`, `furnished`, `rooms`, `minRooms`, `minPrice`, `maxPrice`, `search`; `page`/`limit`), `GET /listings/{id}`, `POST /listings`, `PATCH /listings/{id}`, `DELETE /listings/{id}`, `PATCH /listings/{id}/status`, `POST /listings/{id}/media`

Favorites: `POST /favorites/{id}`, `DELETE /favorites/{id}`, `GET /favorites`

Things worth knowing:

- The `status` enum keeps the original misspellings: `avaiable`, `rented`, `inative`.
- `price` is `NUMERIC(14,2)` (migration 000003 widened it — Naira rents overflowed 10,2).
- Media rows store URLs only; files live on Cloudinary, uploaded straight from the browser.
- Auth is JWT access tokens (HS256) + rotating SHA-256 refresh tokens; reuse of a revoked token revokes all sessions. Passwords are argon2id.
- Emails send via Brevo — see `docs/brevo-email.md`. Avatar flow: `docs/avatar-uploads.md`.
- Rate limits are in-memory (30/min list, 60/min detail per IP, 10/hour create per user).

## Commands

```powershell
task run          # serve (DATABASE_URL required)
task migrate      # apply pending SQL migrations
task migrate-down # roll back the latest migration
task test         # go test ./...
task check        # gofmt + vet + tests
task docs         # regenerate Swagger (needs swag CLI)
```

## Layout

```text
cmd/api        config → db → services → handler → HTTP
cmd/migrate    SQL runner (schema_migrations table)
internal/auth  users, refresh/reset tokens, JWT, argon2id, email sender
internal/listing, internal/favorite
internal/ratelimit
internal/web   decode + validate at the boundary, bearer middleware, CORS
migrations     000001 core tables, 000002 password resets, 000003 wider price
docs           brevo + avatar setup (swagger.* is generated)
```
