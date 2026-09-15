# Sending real email with Brevo

The app sends auth mail through [Brevo](https://www.brevo.com) using the
Transactional SMTP API (`POST /v3/smtp/email`) over plain `net/http` — no
vendor SDK. `auth.EmailSender` (`internal/auth/email.go`) is the only place
that touches Brevo; the service layer just calls `SendVerificationEmail` /
`SendPasswordResetEmail` and treats failures as best-effort (logged, auth
still succeeds).

## 1. Quickstart (3 steps)

1. **Get your API key.** Open [API key settings](https://app.brevo.com/settings/keys/api),
   click **Generate a new API key**, name it (e.g. `easyrent-dev`), and copy it
   immediately — it is only shown once. Every request authenticates with the
   `api-key` header.
2. **Authorize your sender + IP.**
   - Add and verify your sender address under **Senders**: Brevo only sends
     from verified senders/domains.
   - If you call from a browser playground, add your current IP under
     [Authorized IPs](https://app.brevo.com/security/authorised_ips). Not
     needed for server/CLI calls. If the IP is missing, Brevo sends a
     security email with a one-click authorization link.
3. **Send your first request.** Set the key below and hit the API:
   `GET /v3/account` with header `api-key: <key>` should return `200` with
   your account details — the key is valid.

## 2. Config

Add to `.env` / `.env.example`:

```env
BREVO_API_KEY=xkeysib-your-key-here
EMAIL_FROM=EasyRent <noreply@yourdomain.com>
APP_URL=http://localhost:8080
FRONTEND_URL=http://localhost:3000
```

`BREVO_API` is accepted as a fallback alias for `BREVO_API_KEY` (handy if you
pasted the key straight from an existing `.env`). `EMAIL_FROM` accepts
`"Name <addr>"` or a bare address; it is split into Brevo's
`sender: {name, email}` object. Use a verified sender — unverified `From`
addresses are rejected.

`internal/config/config.go` holds:

```go
BrevoAPIKey string
EmailFrom   string
AppURL      string
FrontendURL string
```

## 3. How it is wired (already done)

`internal/auth/email.go` POSTs JSON to `{base}/smtp/email`:

```json
{
  "sender": {"name": "EasyRent", "email": "noreply@yourdomain.com"},
  "to": [{"email": "user@example.com"}],
  "subject": "Confirm your EasyRent email",
  "htmlContent": "<!doctype html>..."
}
```

with headers `api-key`, `Accept: application/json`,
`Content-Type: application/json`. A `200`/`201` with `{"messageId": "..."}`
is success; anything else is returned as an error and logged best-effort by
the auth service. `cmd/api/main.go` fills
`auth.EmailSender{APIKey, From, AppURL: cfg.FrontendURL}` from config, so the
verify/reset links in the mail point at the web app (`FRONTEND_URL`,
defaulting to `APP_URL`), not the API.

Keep tokens out of server logs in production: the `auth email sent`
log line records the Brevo message id, recipient and subject only.

## 4. Testing

1. `task dev`, sign up in Swagger with a real inbox address.
2. The verification mail arrives from your `EMAIL_FROM` sender; open the link
   (or paste the token into `GET /auth/verify`).
3. Same for forgot-password: the reset link arrives within seconds.
4. Brevo's dashboard (**Transactional → Logs**) shows delivery status per
   message; check there first when a mail does not arrive.

Unit tests stub the HTTP layer: `EmailSender{BaseURL, HTTPClient}` points at
an `httptest` server asserting path `/smtp/email`, the `api-key` header, and
the sender/to/subject/htmlContent payload (`internal/auth/email_test.go`).

## 5. Going to production

- Verify your sending domain (SPF/DKIM) under Brevo **Senders & IPs** and
  switch `EMAIL_FROM` to it.
- Restrict the API key scope and rotate it periodically.
- Never commit `.env`: the key lives in the environment / secret manager.
