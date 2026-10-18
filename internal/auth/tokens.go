package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type AccessTokenPayload struct {
	UserID   uuid.UUID
	Email    string
	IssuedAt time.Time
}

type Tokens struct {
	Secret    []byte
	AccessTTL time.Duration
}

type accessClaims struct {
	Email string `json:"email"`
	jwt.RegisteredClaims
}

func (t Tokens) SignAccessToken(userID uuid.UUID, email string) (string, error) {
	now := time.Now()
	claims := accessClaims{
		Email: email,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(t.AccessTTL)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(t.Secret)
}

func (t Tokens) VerifyAccessToken(token string) (AccessTokenPayload, error) {
	var claims accessClaims
	_, err := jwt.ParseWithClaims(token, &claims, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, ErrInvalidToken
		}
		return t.Secret, nil
	})
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return AccessTokenPayload{}, ErrTokenExpired
		}
		return AccessTokenPayload{}, ErrInvalidToken
	}

	id, err := uuid.Parse(claims.Subject)
	if err != nil || claims.Email == "" {
		return AccessTokenPayload{}, ErrInvalidToken
	}
	payload := AccessTokenPayload{UserID: id, Email: claims.Email}
	if claims.IssuedAt != nil {
		payload.IssuedAt = claims.IssuedAt.Time
	}
	return payload, nil
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func GenerateRefreshToken() (string, error) {
	return randomHex(64)
}

func GenerateVerificationToken() (string, error) {
	return randomHex(32)
}

func HashRefreshToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
