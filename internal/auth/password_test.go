package auth

import "testing"

func TestHashPassword_RoundTrip(t *testing.T) {
	hash, err := HashPassword("correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if hash == "correct-horse-battery-staple" {
		t.Fatal("hash must not equal the plaintext password")
	}
	if err := ComparePassword(hash, "correct-horse-battery-staple"); err != nil {
		t.Fatalf("ComparePassword with correct password should succeed, got: %v", err)
	}
}

func TestComparePassword_WrongPassword(t *testing.T) {
	hash, err := HashPassword("correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if err := ComparePassword(hash, "wrong-password"); err == nil {
		t.Fatal("ComparePassword with wrong password should fail")
	}
}

func TestHashPassword_SameInputDifferentHash(t *testing.T) {
	// bcrypt salts each hash, so hashing the same password twice must
	// not produce identical output.
	h1, err := HashPassword("same-password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	h2, err := HashPassword("same-password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if h1 == h2 {
		t.Fatal("hashing the same password twice should produce different salted hashes")
	}
}
