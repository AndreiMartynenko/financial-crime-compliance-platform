package auth

import (
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
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

func TestAuthenticateWithJWKS(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	exponent := big.NewInt(int64(privateKey.PublicKey.E)).Bytes()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(response).Encode(map[string]any{"keys": []map[string]string{{
			"kid": "test-key", "kty": "RSA", "use": "sig",
			"n": base64.RawURLEncoding.EncodeToString(privateKey.PublicKey.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(exponent),
		}}})
	}))
	defer server.Close()

	authenticator, err := NewJWKSAuthenticator(server.URL, "fccp-test", "fccp-web")
	if err != nil {
		t.Fatal(err)
	}
	authenticator.now = func() time.Time { return time.Unix(1_000, 0) }
	token := rsaTestToken(t, privateKey, `{"sub":"analyst@example.test","azp":"fccp-web","realm_access":{"roles":["analyst"]},"iss":"fccp-test","exp":1100}`)
	principal, err := authenticator.Authenticate("Bearer " + token)
	if err != nil || principal.Subject != "analyst@example.test" || principal.Role != RoleAnalyst {
		t.Fatalf("principal=%+v err=%v", principal, err)
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

func rsaTestToken(t *testing.T, privateKey *rsa.PrivateKey, payload string) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT","kid":"test-key"}`))
	body := base64.RawURLEncoding.EncodeToString([]byte(payload))
	unsigned := header + "." + body
	digest := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%s.%s", unsigned, base64.RawURLEncoding.EncodeToString(signature))
}
