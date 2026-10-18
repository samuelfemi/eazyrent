package auth

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// dummyPasswordHash is a valid argon2id hash verified against the submitted
// password when no account matches, so an unknown email costs the same as a
// known one. Without it the fast path leaks which emails are registered.
const dummyPasswordHash = `$argon2id$v=19$m=65536,t=1,p=4$YSvBa7MjbvrSiWbBtvXVfw$Hm4ifzLu/+vtfj9GQFAeq4D7wd8P43RXkO2Sfmhw5MQ`

// normalizeEmail folds an address to the canonical form stored and looked up.
// The users table is unique on lower(email), so this keeps case variants from
// creating duplicate accounts.
func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// AuthTokens is the issued pair returned by sign-up, sign-in and refresh.
type AuthTokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

// VerificationTTL is how long an email-verification link stays valid,
// mirroring the TS 24-hour expiry.
const VerificationTTL = 24 * time.Hour

// ResetTokenTTL is how long a password-reset link stays valid.
const ResetTokenTTL = time.Hour

// pgUniqueViolation is the Postgres error code for unique-constraint
// violations, used to turn a sign-up race into ErrEmailTaken.
const pgUniqueViolation = "23505"

// Service orchestrates the auth flows over a Store, Tokens and an
// EmailSender, mirroring the TS AuthService. It is a concrete type wired
// explicitly in main; handlers call it directly.
type Service struct {
	Users      Store
	Tokens     Tokens
	Email      EmailSender
	RefreshTTL time.Duration
}

func NewService(users Store, tokens Tokens, email EmailSender, refreshTTL time.Duration) Service {
	return Service{Users: users, Tokens: tokens, Email: email, RefreshTTL: refreshTTL}
}

// issueTokens mints an access token and a refresh token, persisting only
// the refresh token's hash, like the TS issueTokens.
func (s Service) issueTokens(ctx context.Context, userID uuid.UUID, email string) (AuthTokens, error) {
	access, err := s.Tokens.SignAccessToken(userID, email)
	if err != nil {
		return AuthTokens{}, err
	}

	rawRefresh, err := GenerateRefreshToken()
	if err != nil {
		return AuthTokens{}, err
	}

	expiresAt := time.Now().Add(s.RefreshTTL)
	if err := s.Users.StoreRefreshToken(ctx, userID, HashRefreshToken(rawRefresh), expiresAt); err != nil {
		return AuthTokens{}, err
	}

	return AuthTokens{AccessToken: access, RefreshToken: rawRefresh}, nil
}

// SignUp registers a user, stores a verification token and sends the
// verification mail best-effort (a mail failure only logs, like the TS
// catch+warn). It returns the first token pair immediately.
func (s Service) SignUp(ctx context.Context, email, password, phone, fullName string) (AuthTokens, error) {
	email = normalizeEmail(email)
	if _, err := s.Users.FindByEmail(ctx, email); err == nil {
		return AuthTokens{}, ErrEmailTaken
	} else if !errors.Is(err, ErrUserNotFound) {
		return AuthTokens{}, err
	}

	passwordHash, err := HashPassword(password)
	if err != nil {
		return AuthTokens{}, err
	}

	user, err := s.Users.CreateUser(ctx, email, phone, passwordHash, fullName)
	if err != nil {
		// Two concurrent sign-ups can both pass the check above; the
		// unique constraint is the arbiter.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			return AuthTokens{}, ErrEmailTaken
		}
		return AuthTokens{}, err
	}

	verificationToken, err := GenerateVerificationToken()
	if err != nil {
		return AuthTokens{}, err
	}
	if err := s.Users.StoreVerificationToken(ctx, user.ID, verificationToken, time.Now().Add(VerificationTTL)); err != nil {
		return AuthTokens{}, err
	}

	if err := s.Email.SendVerificationEmail(user.Email, user.FullName, verificationToken); err != nil {
		log.Printf("auth: send verification email to %s failed: %v", user.Email, err)
	}
	log.Printf("auth event=sign_up user=%s email=%s success=true", user.ID, user.Email)

	return s.issueTokens(ctx, user.ID, user.Email)
}

// SignIn checks credentials and the verified flag, then issues a fresh
// token pair. Unknown emails and wrong passwords both yield
// ErrInvalidCredentials so callers cannot probe for accounts.
func (s Service) SignIn(ctx context.Context, email, password string) (AuthTokens, error) {
	email = normalizeEmail(email)
	user, err := s.Users.FindByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			// Spend the same work as a real account so response time does
			// not reveal whether the email is registered.
			_, _ = VerifyPassword(dummyPasswordHash, password)
			return AuthTokens{}, ErrInvalidCredentials
		}
		return AuthTokens{}, err
	}

	valid, err := VerifyPassword(user.PasswordHash, password)
	if err != nil {
		return AuthTokens{}, err
	}
	if !valid {
		log.Printf("auth event=sign_in user=%s email=%s success=false", user.ID, email)
		return AuthTokens{}, ErrInvalidCredentials
	}

	if !user.EmailVerified {
		return AuthTokens{}, ErrEmailNotVerified
	}

	tokens, err := s.issueTokens(ctx, user.ID, user.Email)
	if err != nil {
		return AuthTokens{}, err
	}
	log.Printf("auth event=sign_in user=%s email=%s success=true", user.ID, user.Email)
	return tokens, nil
}

// Refresh rotates a refresh token: the presented token is revoked and a
// new pair issued. Reusing a revoked token signals theft, so every token
// of the user is revoked, like the TS reuse detection.
func (s Service) Refresh(ctx context.Context, rawRefreshToken string) (AuthTokens, error) {
	stored, err := s.Users.FindRefreshToken(ctx, HashRefreshToken(rawRefreshToken))
	if err != nil {
		if errors.Is(err, ErrRefreshTokenNotFound) {
			return AuthTokens{}, ErrInvalidToken
		}
		return AuthTokens{}, err
	}

	if stored.RevokedAt.Valid {
		if err := s.Users.RevokeAllUserTokens(ctx, stored.UserID); err != nil {
			return AuthTokens{}, err
		}
		return AuthTokens{}, ErrInvalidToken
	}

	if time.Now().After(stored.ExpiresAt) {
		return AuthTokens{}, ErrTokenExpired
	}

	// Claim the token atomically; losing the race to a concurrent rotation
	// means this presentation is stale, so reject it.
	claimed, err := s.Users.ClaimRefreshToken(ctx, stored.ID)
	if err != nil {
		return AuthTokens{}, err
	}
	if !claimed {
		return AuthTokens{}, ErrInvalidToken
	}

	user, err := s.Users.FindByID(ctx, stored.UserID)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return AuthTokens{}, ErrInvalidToken
		}
		return AuthTokens{}, err
	}

	return s.issueTokens(ctx, user.ID, user.Email)
}

// SignOut revokes one refresh token. An unknown token is a no-op: signing
// out is idempotent, like the TS onNone-to-void branch.
func (s Service) SignOut(ctx context.Context, rawRefreshToken string) error {
	stored, err := s.Users.FindRefreshToken(ctx, HashRefreshToken(rawRefreshToken))
	if err != nil {
		if errors.Is(err, ErrRefreshTokenNotFound) {
			return nil
		}
		return err
	}
	return s.Users.RevokeRefreshToken(ctx, stored.ID)
}

// VerifyEmail consumes an email-verification token. Unlike the TS orDie
// pipes, database failures return as errors instead of panicking.
func (s Service) VerifyEmail(ctx context.Context, token string) error {
	user, err := s.Users.FindByVerificationToken(ctx, token)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return ErrInvalidVerificationToken
		}
		return err
	}

	if !user.VerificationTokenExpiresAt.Valid || time.Now().After(user.VerificationTokenExpiresAt.Time) {
		return ErrTokenExpired
	}

	return s.Users.MarkEmailVerified(ctx, user.ID)
}

// RequestPasswordReset starts the forgot-password flow. Unknown emails
// succeed silently: the caller must not learn whether an email is
// registered. The reset mail is sent best-effort, like sign-up mail.
func (s Service) RequestPasswordReset(ctx context.Context, email string) error {
	user, err := s.Users.FindByEmail(ctx, normalizeEmail(email))
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return nil
		}
		return err
	}

	rawToken, err := GenerateVerificationToken()
	if err != nil {
		return err
	}
	if err := s.Users.StorePasswordResetToken(ctx, user.ID, HashRefreshToken(rawToken), time.Now().Add(ResetTokenTTL)); err != nil {
		return err
	}

	if err := s.Email.SendPasswordResetEmail(user.Email, user.FullName, rawToken); err != nil {
		log.Printf("auth: send password-reset email to %s failed: %v", user.Email, err)
	}
	log.Printf("auth event=password_reset_requested user=%s email=%s success=true", user.ID, user.Email)
	return nil
}

// ResetPassword consumes a reset token and sets a new password. The token
// is single-use, and every refresh token of the user is revoked so stolen
// sessions die with the old password.
func (s Service) ResetPassword(ctx context.Context, rawToken, newPassword string) error {
	stored, err := s.Users.FindPasswordResetToken(ctx, HashRefreshToken(rawToken))
	if err != nil {
		return err
	}

	if stored.UsedAt.Valid {
		return ErrInvalidResetToken
	}
	if time.Now().After(stored.ExpiresAt) {
		return ErrTokenExpired
	}

	passwordHash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}

	if err := s.Users.CompletePasswordReset(ctx, stored.ID, stored.UserID, passwordHash); err != nil {
		return err
	}

	log.Printf("auth event=password_reset user=%s success=true", stored.UserID)
	return nil
}
