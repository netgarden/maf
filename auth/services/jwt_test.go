package services

import (
	"strings"
	"testing"
	"time"
)

const testSecret = "test-secret-key"

func TestCreateAndParseAccessToken_RoundTrip(t *testing.T) {
	userID := "user-xyz-456"

	token, err := createAccessToken(testSecret, userID, 15*time.Minute)
	if err != nil {
		t.Fatalf("createAccessToken: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	claims, err := parseAccessToken(testSecret, token)
	if err != nil {
		t.Fatalf("parseAccessToken: %v", err)
	}
	if claims.UserID != userID {
		t.Errorf("UserID: want %q, got %q", userID, claims.UserID)
	}
	if !claims.ExpiresAt.After(claims.IssuedAt.Time) {
		t.Error("ExpiresAt must be after IssuedAt")
	}
}

func TestParseAccessToken_Expired(t *testing.T) {
	token, _ := createAccessToken(testSecret, "user-1", -time.Second)
	_, err := parseAccessToken(testSecret, token)
	if err == nil {
		t.Fatal("expected error for expired access token")
	}
	if !strings.Contains(err.Error(), "expired") {
		t.Errorf("error should mention expiry, got: %v", err)
	}
}

func TestParseAccessToken_WrongSecret(t *testing.T) {
	token, _ := createAccessToken(testSecret, "user-1", time.Minute)
	_, err := parseAccessToken("other-secret", token)
	if err == nil {
		t.Fatal("expected error for wrong secret")
	}
}

func TestParseAccessToken_TamperedSignature(t *testing.T) {
	token, _ := createAccessToken(testSecret, "user-1", time.Minute)
	parts := strings.Split(token, ".")
	parts[2] += "X"
	_, err := parseAccessToken(testSecret, strings.Join(parts, "."))
	if err == nil {
		t.Fatal("expected error for tampered signature")
	}
}

func TestParseAccessToken_InvalidFormat(t *testing.T) {
	for _, bad := range []string{"", "only.two", "not-a-jwt"} {
		_, err := parseAccessToken(testSecret, bad)
		if err == nil {
			t.Errorf("expected error for %q", bad)
		}
	}
}
