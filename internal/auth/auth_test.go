package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"testing"
	"time"
)

const testSecret = "test-secret-with-at-least-32-characters"

func TestAuthenticate(t *testing.T) {
	authenticator, err := NewAuthenticator(testSecret, "fccp-test")
	if err != nil {
		t.Fatal(err)
	}
	authenticator.now = func() time.Time { return time.Unix(1_000, 0) }
	token := testToken(`{"sub":"analyst@example.test","role":"analyst","iss":"fccp-test","exp":1100}`)
	principal, err := authenticator.Authenticate("Bearer " + token)
	if err != nil || principal.Subject != "analyst@example.test" || principal.Role != RoleAnalyst {
		t.Fatalf("principal=%+v err=%v", principal, err)
	}
}

func TestAuthenticateRejectsInvalidTokens(t *testing.T) {
	authenticator, err := NewAuthenticator(testSecret, "fccp-test")
	if err != nil {
		t.Fatal(err)
	}
	authenticator.now = func() time.Time { return time.Unix(1_000, 0) }
	tests := []string{
		"",
		"Bearer invalid",
		"Bearer " + testToken(`{"sub":"user","role":"analyst","iss":"fccp-test","exp":999}`),
		"Bearer " + testToken(`{"sub":"user","role":"owner","iss":"fccp-test","exp":1100}`),
		"Bearer " + testToken(`{"sub":"user","role":"analyst","iss":"other","exp":1100}`),
	}
	for _, authorization := range tests {
		if _, err := authenticator.Authenticate(authorization); !errors.Is(err, ErrInvalidToken) {
			t.Fatalf("authorization=%q err=%v", authorization, err)
		}
	}
}

func testToken(payload string) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	body := base64.RawURLEncoding.EncodeToString([]byte(payload))
	unsigned := header + "." + body
	mac := hmac.New(sha256.New, []byte(testSecret))
	_, _ = mac.Write([]byte(unsigned))
	return fmt.Sprintf("%s.%s", unsigned, base64.RawURLEncoding.EncodeToString(mac.Sum(nil)))
}
