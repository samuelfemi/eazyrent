package auth

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrUserNotFound         = errors.New("user not found")
	ErrRefreshTokenNotFound = errors.New("refresh token not found")
)

type User struct {
	ID                         uuid.UUID      `json:"id"`
	Email                      string         `json:"email"`
	Phone                      string         `json:"phone"`
	PasswordHash               string         `json:"-"`
	FullName                   string         `json:"full_name"`
	AvatarURL                  sql.NullString `json:"avatar_url"`
	EmailVerified              bool           `json:"email_verified"`
	VerificationToken          sql.NullString `json:"verification_token"`
	VerificationTokenExpiresAt sql.NullTime   `json:"verification_token_expires_at"`
	// TokensValidAfter, when set, is the instant before which access tokens
	// are rejected even though JWTs are stateless. Bumped when every session
	// must die (password reset, refresh-token reuse).
	TokensValidAfter sql.NullTime `json:"-"`
	CreatedAt        time.Time    `json:"created_at"`
	UpdatedAt        time.Time    `json:"updated_at"`
}

type RefreshToken struct {
	ID        uuid.UUID    `json:"id"`
	UserID    uuid.UUID    `json:"user_id"`
	ExpiresAt time.Time    `json:"expires_at"`
	RevokedAt sql.NullTime `json:"revoked_at"`
}

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) Store {
	return Store{db: db}
}

const userColumns = `id, email, phone, password_hash, full_name, avatar_url,
	email_verified, verification_token, verification_token_expires_at,
	tokens_valid_after, created_at, updated_at`

type scanner interface {
	Scan(dest ...any) error
}

func scanUser(s scanner) (User, error) {
	var u User
	err := s.Scan(
		&u.ID,
		&u.Email,
		&u.Phone,
		&u.PasswordHash,
		&u.FullName,
		&u.AvatarURL,
		&u.EmailVerified,
		&u.VerificationToken,
		&u.VerificationTokenExpiresAt,
		&u.TokensValidAfter,
		&u.CreatedAt,
		&u.UpdatedAt,
	)
	return u, err
}

const findUserByEmailQuery = `
	SELECT ` + userColumns + `
	FROM users
	WHERE lower(email) = lower($1)
	LIMIT 1
`

func (s Store) FindByEmail(ctx context.Context, email string) (User, error) {
	u, err := scanUser(s.db.QueryRowContext(ctx, findUserByEmailQuery, email))
	if err == sql.ErrNoRows {
		return User{}, ErrUserNotFound
	}
	if err != nil {
		return User{}, err
	}
	return u, nil
}

const findUserByIDQuery = `
	SELECT ` + userColumns + `
	FROM users
	WHERE id = $1
	LIMIT 1
`

func (s Store) FindByID(ctx context.Context, id uuid.UUID) (User, error) {
	u, err := scanUser(s.db.QueryRowContext(ctx, findUserByIDQuery, id))
	if err == sql.ErrNoRows {
		return User{}, ErrUserNotFound
	}
	if err != nil {
		return User{}, err
	}
	return u, nil
}

const createUserQuery = `
	INSERT INTO users (email, phone, password_hash, full_name)
	VALUES ($1, $2, $3, $4)
	RETURNING ` + userColumns + `
`

const updateAvatarQuery = `
	UPDATE users
	SET avatar_url = $2, updated_at = now()
	WHERE id = $1
`

func (s Store) UpdateAvatar(ctx context.Context, id uuid.UUID, url string) error {
	var avatar sql.NullString
	if url != "" {
		avatar = sql.NullString{String: url, Valid: true}
	}
	_, err := s.db.ExecContext(ctx, updateAvatarQuery, id, avatar)
	return err
}

func (s Store) CreateUser(ctx context.Context, email, phone, passwordHash, fullName string) (User, error) {
	u, err := scanUser(s.db.QueryRowContext(ctx, createUserQuery, email, phone, passwordHash, fullName))
	if err != nil {
		return User{}, err
	}
	return u, nil
}

const storeRefreshTokenQuery = `
	INSERT INTO refresh_tokens (user_id, token_hash, expires_at)
	VALUES ($1, $2, $3)
`

func (s Store) StoreRefreshToken(ctx context.Context, userID uuid.UUID, tokenHash string, expiresAt time.Time) error {
	_, err := s.db.ExecContext(ctx, storeRefreshTokenQuery, userID, tokenHash, expiresAt)
	return err
}

const findRefreshTokenQuery = `
	SELECT id, user_id, expires_at, revoked_at
	FROM refresh_tokens
	WHERE token_hash = $1
	LIMIT 1
`

func (s Store) FindRefreshToken(ctx context.Context, tokenHash string) (RefreshToken, error) {
	var t RefreshToken
	err := s.db.QueryRowContext(ctx, findRefreshTokenQuery, tokenHash).
		Scan(&t.ID, &t.UserID, &t.ExpiresAt, &t.RevokedAt)
	if err == sql.ErrNoRows {
		return RefreshToken{}, ErrRefreshTokenNotFound
	}
	if err != nil {
		return RefreshToken{}, err
	}
	return t, nil
}

const revokeRefreshTokenQuery = `
	UPDATE refresh_tokens
	SET revoked_at = now()
	WHERE id = $1
`

func (s Store) RevokeRefreshToken(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.ExecContext(ctx, revokeRefreshTokenQuery, id)
	return err
}

const claimRefreshTokenQuery = `
	UPDATE refresh_tokens
	SET revoked_at = now()
	WHERE id = $1 AND revoked_at IS NULL
`

// ClaimRefreshToken atomically revokes a token only if it is still live,
// reporting whether this call won the race. Concurrent rotations of the same
// token therefore succeed exactly once.
func (s Store) ClaimRefreshToken(ctx context.Context, id uuid.UUID) (bool, error) {
	res, err := s.db.ExecContext(ctx, claimRefreshTokenQuery, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

const revokeAllUserTokensQuery = `
	UPDATE refresh_tokens
	SET revoked_at = now()
	WHERE user_id = $1 AND revoked_at IS NULL
`

// bumpTokensValidAfter fences out access tokens minted before now, using
// second precision to match the JWT IssuedAt claim.
const bumpTokensValidAfterQuery = `
	UPDATE users
	SET tokens_valid_after = date_trunc('second', now())
	WHERE id = $1
`

// RevokeAllUserTokens kills every session of a user: all refresh tokens are
// revoked and, in the same transaction, the access-token watermark is bumped
// so already-issued access tokens stop working too.
func (s Store) RevokeAllUserTokens(ctx context.Context, userID uuid.UUID) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, revokeAllUserTokensQuery, userID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, bumpTokensValidAfterQuery, userID); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteExpiredSessions sweeps refresh and password-reset tokens that are
// past expiry, returning how many rows each delete removed.
func (s Store) DeleteExpiredSessions(ctx context.Context) (refresh, reset int64, err error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM refresh_tokens WHERE expires_at < now()`)
	if err != nil {
		return 0, 0, err
	}
	if refresh, err = res.RowsAffected(); err != nil {
		return 0, 0, err
	}

	res, err = s.db.ExecContext(ctx, `DELETE FROM password_reset_tokens WHERE expires_at < now()`)
	if err != nil {
		return 0, 0, err
	}
	if reset, err = res.RowsAffected(); err != nil {
		return 0, 0, err
	}
	return refresh, reset, nil
}

const storeVerificationTokenQuery = `
	UPDATE users
	SET verification_token = $2, verification_token_expires_at = $3
	WHERE id = $1
`

func (s Store) StoreVerificationToken(ctx context.Context, userID uuid.UUID, token string, expiresAt time.Time) error {
	_, err := s.db.ExecContext(ctx, storeVerificationTokenQuery, userID, token, expiresAt)
	return err
}

const findUserByVerificationTokenQuery = `
	SELECT ` + userColumns + `
	FROM users
	WHERE verification_token = $1
	LIMIT 1
`

func (s Store) FindByVerificationToken(ctx context.Context, token string) (User, error) {
	u, err := scanUser(s.db.QueryRowContext(ctx, findUserByVerificationTokenQuery, token))
	if err == sql.ErrNoRows {
		return User{}, ErrUserNotFound
	}
	if err != nil {
		return User{}, err
	}
	return u, nil
}

const markEmailVerifiedQuery = `
	UPDATE users
	SET email_verified = TRUE, verification_token = NULL, verification_token_expires_at = NULL
	WHERE id = $1
`

func (s Store) MarkEmailVerified(ctx context.Context, userID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx, markEmailVerifiedQuery, userID)
	return err
}
