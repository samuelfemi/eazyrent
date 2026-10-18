DROP INDEX IF EXISTS password_reset_tokens_expires_idx;
DROP INDEX IF EXISTS refresh_tokens_expires_idx;
DROP INDEX IF EXISTS users_email_lower_key;
ALTER TABLE users DROP COLUMN IF EXISTS tokens_valid_after;
