-- Access-token revocation watermark. JWTs are stateless, so a plain
-- sign-out/reset cannot invalidate an already-issued access token; the
-- middleware rejects any token issued before this instant instead.
ALTER TABLE users ADD COLUMN tokens_valid_after TIMESTAMPTZ;

-- Case-insensitive accounts: fold existing rows, then enforce uniqueness on
-- lower(email) so Foo@x.com and foo@x.com are the same person. (If two rows
-- already differ only by case this UPDATE fails — resolve them first.)
UPDATE users SET email = lower(email);
CREATE UNIQUE INDEX users_email_lower_key ON users (lower(email));

-- Support the expired-token sweeper.
CREATE INDEX refresh_tokens_expires_idx ON refresh_tokens (expires_at);
CREATE INDEX password_reset_tokens_expires_idx ON password_reset_tokens (expires_at);
