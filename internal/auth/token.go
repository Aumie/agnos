package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const (
	AccessTokenTTL  = 15 * time.Minute
	RefreshTokenTTL = 7 * 24 * time.Hour
)

var ErrInvalidAccessToken = errors.New("auth: invalid or expired access token")

// AccessClaims are exactly what api-spec.md's Auth scheme section commits
// to: sub (staff id, via the standard RegisteredClaims field) and
// hospital_id. Both are embedded so a request can be scoped without a DB
// round-trip — see api-spec.md for why that's an acceptable tradeoff here.
type AccessClaims struct {
	HospitalID uuid.UUID `json:"hospital_id"`
	jwt.RegisteredClaims
}

// IssueAccessToken signs a short-lived access token for staffID scoped to
// hospitalID.
func IssueAccessToken(secret []byte, staffID, hospitalID uuid.UUID) (string, error) {
	now := time.Now()
	claims := AccessClaims{
		HospitalID: hospitalID,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   staffID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(AccessTokenTTL)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(secret)
}

// ParseAccessToken verifies signature and expiry and returns the claims.
func ParseAccessToken(secret []byte, tokenString string) (*AccessClaims, error) {
	claims := &AccessClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidAccessToken
		}
		return secret, nil
	})
	if err != nil || !token.Valid {
		return nil, ErrInvalidAccessToken
	}
	return claims, nil
}

// GenerateRefreshToken returns a high-entropy opaque token. Unlike the
// access token, this is never parsed or verified offline — every refresh
// always hits the DB to check the stored hash and rotate it, so there's no
// benefit to it being a self-describing JWT (and one fewer place an
// algorithm-confusion-style bug could live).
func GenerateRefreshToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// HashToken hashes a refresh token for storage. SHA-256, not bcrypt:
// refresh tokens are already high-entropy random values, not
// human-memorable passwords, so there's nothing for a slow hash to
// protect against here that a fast cryptographic hash doesn't already
// cover — see internal/staff for where this is stored and rotated.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
