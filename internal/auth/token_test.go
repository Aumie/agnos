package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

var testSecret = []byte("test-secret-do-not-use-in-prod")

func TestAccessToken_RoundTrip(t *testing.T) {
	staffID := uuid.New()
	hospitalID := uuid.New()

	token, err := IssueAccessToken(testSecret, staffID, hospitalID)
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}

	claims, err := ParseAccessToken(testSecret, token)
	if err != nil {
		t.Fatalf("ParseAccessToken: %v", err)
	}
	if claims.Subject != staffID.String() {
		t.Errorf("sub = %q, want %q", claims.Subject, staffID.String())
	}
	if claims.HospitalID != hospitalID {
		t.Errorf("hospital_id = %v, want %v", claims.HospitalID, hospitalID)
	}
}

func TestParseAccessToken_WrongSecret(t *testing.T) {
	token, err := IssueAccessToken(testSecret, uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}

	if _, err := ParseAccessToken([]byte("a-completely-different-secret"), token); err == nil {
		t.Fatal("ParseAccessToken with the wrong secret should fail")
	}
}

func TestParseAccessToken_Expired(t *testing.T) {
	claims := AccessClaims{
		HospitalID: uuid.New(),
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   uuid.New().String(),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * AccessTokenTTL)),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-AccessTokenTTL)), // expired 15min ago
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(testSecret)
	if err != nil {
		t.Fatalf("SignedString: %v", err)
	}

	if _, err := ParseAccessToken(testSecret, signed); err == nil {
		t.Fatal("ParseAccessToken with an expired token should fail")
	}
}

func TestParseAccessToken_Malformed(t *testing.T) {
	if _, err := ParseAccessToken(testSecret, "not-a-jwt-at-all"); err == nil {
		t.Fatal("ParseAccessToken with a malformed string should fail")
	}
}

func TestParseAccessToken_RejectsAlgNone(t *testing.T) {
	// alg=none is a classic JWT library footgun — reject it explicitly
	// rather than trusting the token's own header to say how to verify it.
	token := jwt.NewWithClaims(jwt.SigningMethodNone, AccessClaims{
		HospitalID: uuid.New(),
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   uuid.New().String(),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(AccessTokenTTL)),
		},
	})
	signed, err := token.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("SignedString: %v", err)
	}

	if _, err := ParseAccessToken(testSecret, signed); err == nil {
		t.Fatal("ParseAccessToken must reject alg=none tokens")
	}
}

func TestGenerateRefreshToken_Unique(t *testing.T) {
	a, err := GenerateRefreshToken()
	if err != nil {
		t.Fatalf("GenerateRefreshToken: %v", err)
	}
	b, err := GenerateRefreshToken()
	if err != nil {
		t.Fatalf("GenerateRefreshToken: %v", err)
	}
	if a == b {
		t.Fatal("two generated refresh tokens should not collide")
	}
	if len(a) < 32 {
		t.Fatalf("refresh token looks too short to be high-entropy: %q", a)
	}
}

func TestHashToken_Deterministic(t *testing.T) {
	token := "some-refresh-token-value"
	if HashToken(token) != HashToken(token) {
		t.Fatal("HashToken must be deterministic so the stored hash can be matched on refresh")
	}
	if HashToken(token) == token {
		t.Fatal("HashToken must not return the token unchanged")
	}
}
