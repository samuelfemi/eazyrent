package auth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// BrevoAPIBase is the default base URL for Brevo's v3 API.
// Override EmailSender.BaseURL in tests to point at a stub server.
const BrevoAPIBase = "https://api.brevo.com/v3"

type EmailSender struct {
	APIKey string
	From   string
	AppURL string
	// BaseURL overrides BrevoAPIBase (tests). Empty means BrevoAPIBase.
	BaseURL string
	// HTTPClient overrides the default client (tests). Nil means a
	// short-timeout default client.
	HTTPClient *http.Client
}

func (s EmailSender) send(to, subject, htmlBody string) error {
	senderName, senderEmail := parseSender(s.From)
	if senderEmail == "" {
		return fmt.Errorf("invalid EMAIL_FROM %q: need \"Name <addr@example.com>\" or \"addr@example.com\"", s.From)
	}

	payload := map[string]any{
		"sender":      map[string]string{"name": senderName, "email": senderEmail},
		"to":          []map[string]string{{"email": to}},
		"subject":     subject,
		"htmlContent": htmlBody,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	base := s.BaseURL
	if base == "" {
		base = BrevoAPIBase
	}
	client := s.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}

	req, err := http.NewRequest(http.MethodPost, strings.TrimSuffix(base, "/")+"/smtp/email", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("api-key", s.APIKey)

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("brevo smtp/email: status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var out struct {
		MessageID string `json:"messageId"`
	}
	_ = json.Unmarshal(respBody, &out)
	log.Printf("auth email sent id=%s to=%s subject=%q", out.MessageID, to, subject)
	return nil
}

// parseSender splits "Name <addr@example.com>" or a bare address into
// display name and email parts for Brevo's sender object.
func parseSender(from string) (name, email string) {
	from = strings.TrimSpace(from)
	if from == "" {
		return "", ""
	}
	if i := strings.LastIndex(from, "<"); i >= 0 {
		if j := strings.Index(from[i:], ">"); j >= 0 {
			email = strings.TrimSpace(from[i+1 : i+j])
			name = strings.TrimSpace(strings.Trim(from[:i], `"' `))
			return name, email
		}
	}
	return "", from
}

func (s EmailSender) SendVerificationEmail(to, fullName, token string) error {
	link := verificationLink(s.AppURL, token)
	return s.send(to, "Confirm your EasyRent email", verificationHTML(fullName, link))
}

func (s EmailSender) SendPasswordResetEmail(to, fullName, token string) error {
	link := resetLink(s.AppURL, token)
	return s.send(to, "Reset your EasyRent password", resetHTML(fullName, link))
}

func verificationLink(appURL, token string) string {
	return fmt.Sprintf("%s/auth/verify?token=%s", appURL, url.QueryEscape(token))
}

func resetLink(appURL, token string) string {
	return fmt.Sprintf("%s/reset-password?token=%s", appURL, url.QueryEscape(token))
}

func shell(preheader, heading, introHTML, ctaLabel, ctaHref, fallbackHref, expiryNote, footerNote string) string {
	return fmt.Sprintf(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta http-equiv="x-ua-compatible" content="ie=edge">
<title>%s</title>
</head>
<body style="margin:0;padding:0;background-color:#f1f5f9;">
<div style="display:none;max-height:0;overflow:hidden;opacity:0;">%s</div>
<table role="presentation" cellpadding="0" cellspacing="0" border="0" width="100%%" style="background-color:#f1f5f9;padding:32px 16px;">
<tr><td align="center">
<table role="presentation" cellpadding="0" cellspacing="0" border="0" width="600" style="width:100%%;max-width:600px;">
<!-- header -->
<tr><td style="padding:0 0 20px 0;" align="center">
<table role="presentation" cellpadding="0" cellspacing="0" border="0" width="100%%">
<tr><td align="left" style="padding:8px 4px;">
<span style="display:inline-block;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:13px;font-weight:800;letter-spacing:0.14em;color:#0f172a;">EASYRENT</span>
<span style="display:inline-block;margin-left:8px;width:6px;height:6px;border-radius:999px;background-color:#2563eb;vertical-align:middle;"></span>
</td>
<td align="right" style="padding:8px 4px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:12px;color:#64748b;">Nigeria · Lagos</td>
</tr>
</table>
</td></tr>
<!-- card -->
<tr><td style="background-color:#ffffff;border:1px solid #e2e8f0;border-radius:16px;overflow:hidden;">
<!-- hero bar -->
<div style="height:4px;line-height:4px;background-color:#2563eb;">&nbsp;</div>
<table role="presentation" cellpadding="0" cellspacing="0" border="0" width="100%%">
<tr><td style="padding:36px 36px 8px 36px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;">
<h1 style="margin:0;font-size:22px;line-height:28px;font-weight:700;color:#0f172a;letter-spacing:-0.02em;">%s</h1>
</td></tr>
<tr><td style="padding:12px 36px 0 36px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:15px;line-height:24px;color:#334155;">
%s
</td></tr>
<tr><td style="padding:28px 36px 0 36px;" align="left">
<table role="presentation" cellpadding="0" cellspacing="0" border="0">
<tr><td align="center" style="border-radius:10px;background-color:#2563eb;">
<a href="%s" target="_blank" style="display:inline-block;padding:13px 28px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:14px;font-weight:700;line-height:20px;color:#ffffff;text-decoration:none;border-radius:10px;">%s</a>
</td></tr>
</table>
</td></tr>
<!-- fallback link -->
<tr><td style="padding:20px 36px 0 36px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:12px;line-height:18px;color:#64748b;">
If the button doesn't work, copy and paste this link into your browser:<br>
<a href="%s" target="_blank" style="color:#2563eb;word-break:break-all;text-decoration:underline;">%s</a>
</td></tr>
<!-- expiry callout -->
<tr><td style="padding:24px 36px 0 36px;">
<table role="presentation" cellpadding="0" cellspacing="0" border="0" width="100%%" style="background-color:#f8fafc;border:1px solid #e2e8f0;border-radius:10px;">
<tr><td style="padding:12px 14px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:12px;line-height:16px;color:#475569;">
<span style="display:inline-block;width:7px;height:7px;border-radius:999px;background-color:#f59e0b;vertical-align:middle;margin-right:6px;"></span>%s
</td></tr>
</table>
</td></tr>
<tr><td style="padding:20px 36px 32px 36px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:12px;line-height:18px;color:#64748b;">
%s
</td></tr>
</table>
</td></tr>
<!-- footer -->
<tr><td style="padding:20px 8px 0 8px;" align="center">
<p style="margin:0;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:11px;line-height:16px;color:#94a3b8;">
© %s EasyRent · Rental listings for the Nigerian market<br>
You received this because an action was taken on your EasyRent account. If this wasn't you, you can safely ignore this email.
</p>
</td></tr>
</table>
</td></tr>
</table>
</body>
</html>`,
		html.EscapeString(heading),
		html.EscapeString(preheader),
		html.EscapeString(heading),
		introHTML,
		html.EscapeString(ctaHref),
		html.EscapeString(ctaLabel),
		html.EscapeString(fallbackHref),
		html.EscapeString(fallbackHref),
		html.EscapeString(expiryNote),
		footerNote,
		"2026",
	)
}

func VerificationHTML(fullName, link string) string { return verificationHTML(fullName, link) }

func verificationHTML(fullName, link string) string {
	name := html.EscapeString(fullName)
	if name == "" {
		name = "there"
	}
	intro := fmt.Sprintf(
		`<p style="margin:0 0 12px 0;">Hi %s,</p><p style="margin:0;">Thanks for joining EasyRent — you're one tap away from browsing verified rentals across Lagos and beyond. Confirm your email to activate your account and start saving favourites.</p>`,
		name,
	)
	return shell(
		"Confirm your EasyRent email — link expires in 24 hours.",
		"Confirm your email",
		intro,
		"Verify email",
		link,
		link,
		"Link expires in 24 hours and can only be used once.",
		`<p style="margin:0;">Need help? Just reply to this email — we read every message.</p>`,
	)
}

func ResetHTML(fullName, link string) string { return resetHTML(fullName, link) }

func resetHTML(fullName, link string) string {
	name := html.EscapeString(fullName)
	if name == "" {
		name = "there"
	}
	intro := fmt.Sprintf(
		`<p style="margin:0 0 12px 0;">Hi %s,</p><p style="margin:0;">We got a request to reset the password for your EasyRent account. Tap the button below to choose a new one. If you didn't ask for this, no action is needed — your current password still works.</p>`,
		name,
	)
	return shell(
		"Reset your EasyRent password — link expires in 1 hour.",
		"Reset your password",
		intro,
		"Reset password",
		link,
		link,
		"Link expires in 1 hour and can only be used once. After resetting, all other sessions are signed out.",
		`<p style="margin:0;">Didn't request this? Your account is still secure — just ignore this email.</p>`,
	)
}
