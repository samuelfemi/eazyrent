package auth

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"
)

type PasswordResetToken struct {
	ID        uuid.UUID    `json:"id"`
	UserID    uuid.UUID    `json:"user_id"`
	ExpiresAt time.Time    `json:"expires_at"`
	UsedAt    sql.NullTime `json:"used_at"`
	CreatedAt time.Time    `json:"created_at"`
}

const storePasswordResetTokenQuery = `
	INSERT INTO password_reset_tokens (user_id, token_hash, expires_at)
	VALUES ($1, $2, $3)
`

func (s Store) StorePasswordResetToken(ctx context.Context, userID uuid.UUID, tokenHash string, expiresAt time.Time) error {
	_, err := s.db.ExecContext(ctx, storePasswordResetTokenQuery, userID, tokenHash, expiresAt)
	return err
}

const findPasswordResetTokenQuery = `
	SELECT id, user_id, expires_at, used_at, created_at
	FROM password_reset_tokens
	WHERE token_hash = $1
	LIMIT 1
`

func (s Store) FindPasswordResetToken(ctx context.Context, tokenHash string) (PasswordResetToken, error) {
	var t PasswordResetToken
	err := s.db.QueryRowContext(ctx, findPasswordResetTokenQuery, tokenHash).
		Scan(&t.ID, &t.UserID, &t.ExpiresAt, &t.UsedAt, &t.CreatedAt)
	if err == sql.ErrNoRows {
		return PasswordResetToken{}, ErrInvalidResetToken
	}
	if err != nil {
		return PasswordResetToken{}, err
	}
	return t, nil
}

const claimPasswordResetTokenQuery = `
	UPDATE password_reset_tokens
	SET used_at = now()
	WHERE id = $1 AND used_at IS NULL
`

const updatePasswordQuery = `
	UPDATE users
	SET password_hash = $2, updated_at = now()
	WHERE id = $1
`

// CompletePasswordReset applies a reset atomically: it claims the reset token
// (single-use, race-safe), sets the new password, fences out existing access
// tokens and revokes every refresh token. A partial application cannot happen,
// so a failed step never leaves a reusable token or a live stolen session.
func (s Store) CompletePasswordReset(ctx context.Context, tokenID, userID uuid.UUID, passwordHash string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx, claimPasswordResetTokenQuery, tokenID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrInvalidResetToken
	}

	if _, err := tx.ExecContext(ctx, updatePasswordQuery, userID, passwordHash); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, revokeAllUserTokensQuery, userID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, bumpTokensValidAfterQuery, userID); err != nil {
		return err
	}
	return tx.Commit()
}
