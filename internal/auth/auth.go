package auth

import (
	"context"
	"crypto"
	"crypto/hmac"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Role string

const (
	RoleAnalyst  Role = "analyst"
	RoleReviewer Role = "reviewer"
	RoleAdmin    Role = "admin"
)

var ErrInvalidToken = errors.New("invalid access token")

type Principal struct {
	Subject string
	Role    Role
}

type claims struct {
	Subject         string `json:"sub"`
	Role            Role   `json:"role"`
	Issuer          string `json:"iss"`
	ExpiresAt       int64  `json:"exp"`
	NotBefore       int64  `json:"nbf,omitempty"`
	AuthorizedParty string `json:"azp,omitempty"`
	RealmAccess     struct {
		Roles []Role `json:"roles"`
	} `json:"realm_access,omitempty"`
}

type header struct {
	Algorithm string `json:"alg"`
	Type      string `json:"typ"`
	KeyID     string `json:"kid,omitempty"`
}

type Authenticator struct {
	secret          []byte
	issuer          string
	now             func() time.Time
	jwksURL         string
	authorizedParty string
	keysMu          sync.RWMutex
	keys            map[string]*rsa.PublicKey
	keysExpiresAt   time.Time
}

func NewJWKSAuthenticator(jwksURL, issuer, authorizedParty string) (*Authenticator, error) {
	if strings.TrimSpace(jwksURL) == "" || strings.TrimSpace(issuer) == "" || strings.TrimSpace(authorizedParty) == "" {
		return nil, errors.New("JWT_JWKS_URL, JWT_ISSUER and JWT_AUTHORIZED_PARTY are required")
	}
	return &Authenticator{jwksURL: jwksURL, issuer: issuer, authorizedParty: authorizedParty, now: time.Now, keys: make(map[string]*rsa.PublicKey)}, nil
}

func NewAuthenticator(secret, issuer string) (*Authenticator, error) {
	if len(secret) < 32 {
		return nil, errors.New("JWT_SECRET must contain at least 32 characters")
	}
	if strings.TrimSpace(issuer) == "" {
		return nil, errors.New("JWT_ISSUER is required")
	}
	return &Authenticator{secret: []byte(secret), issuer: issuer, now: time.Now}, nil
}

func (a *Authenticator) Authenticate(authorization string) (Principal, error) {
	scheme, token, ok := strings.Cut(strings.TrimSpace(authorization), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(token) == "" {
		return Principal{}, ErrInvalidToken
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Principal{}, ErrInvalidToken
	}

	var tokenHeader header
	if err := decodePart(parts[0], &tokenHeader); err != nil || (tokenHeader.Type != "" && tokenHeader.Type != "JWT") {
		return Principal{}, ErrInvalidToken
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return Principal{}, ErrInvalidToken
	}
	unsigned := []byte(parts[0] + "." + parts[1])
	if !a.validSignature(tokenHeader, unsigned, signature) {
		return Principal{}, ErrInvalidToken
	}

	var claims claims
	if err := decodePart(parts[1], &claims); err != nil {
		return Principal{}, ErrInvalidToken
	}
	now := a.now().Unix()
	role := claims.Role
	if !validRole(role) {
		for _, candidate := range claims.RealmAccess.Roles {
			if validRole(candidate) {
				role = candidate
				break
			}
		}
	}
	if strings.TrimSpace(claims.Subject) == "" || claims.Issuer != a.issuer || claims.ExpiresAt <= now || claims.NotBefore > now || !validRole(role) || (a.authorizedParty != "" && claims.AuthorizedParty != a.authorizedParty) {
		return Principal{}, ErrInvalidToken
	}
	return Principal{Subject: claims.Subject, Role: role}, nil
}

func (a *Authenticator) validSignature(header header, unsigned, signature []byte) bool {
	if header.Algorithm == "HS256" && len(a.secret) > 0 {
		mac := hmac.New(sha256.New, a.secret)
		_, _ = mac.Write(unsigned)
		return hmac.Equal(signature, mac.Sum(nil))
	}
	if header.Algorithm != "RS256" || header.KeyID == "" || a.jwksURL == "" {
		return false
	}
	key, err := a.signingKey(header.KeyID)
	if err != nil {
		return false
	}
	digest := sha256.Sum256(unsigned)
	return rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature) == nil
}

func (a *Authenticator) signingKey(keyID string) (*rsa.PublicKey, error) {
	a.keysMu.RLock()
	key := a.keys[keyID]
	fresh := a.now().Before(a.keysExpiresAt)
	a.keysMu.RUnlock()
	if key != nil && fresh {
		return key, nil
	}
	if err := a.refreshKeys(key == nil); err != nil {
		return nil, err
	}
	a.keysMu.RLock()
	defer a.keysMu.RUnlock()
	key = a.keys[keyID]
	if key == nil {
		return nil, errors.New("signing key not found")
	}
	return key, nil
}

func (a *Authenticator) refreshKeys(force bool) error {
	a.keysMu.Lock()
	defer a.keysMu.Unlock()
	if !force && a.now().Before(a.keysExpiresAt) && len(a.keys) > 0 {
		return nil
	}
	client := http.Client{Timeout: 3 * time.Second}
	response, err := client.Get(a.jwksURL)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("JWKS endpoint returned %d", response.StatusCode)
	}
	var set struct {
		Keys []struct {
			ID   string `json:"kid"`
			Type string `json:"kty"`
			Use  string `json:"use"`
			N    string `json:"n"`
			E    string `json:"e"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(response.Body).Decode(&set); err != nil {
		return err
	}
	keys := make(map[string]*rsa.PublicKey)
	for _, item := range set.Keys {
		if item.Type != "RSA" || item.ID == "" {
			continue
		}
		nBytes, nErr := base64.RawURLEncoding.DecodeString(item.N)
		eBytes, eErr := base64.RawURLEncoding.DecodeString(item.E)
		if nErr != nil || eErr != nil {
			continue
		}
		exponent := new(big.Int).SetBytes(eBytes).Int64()
		if exponent < 2 {
			continue
		}
		keys[item.ID] = &rsa.PublicKey{N: new(big.Int).SetBytes(nBytes), E: int(exponent)}
	}
	if len(keys) == 0 {
		return errors.New("JWKS contains no RSA signing keys")
	}
	a.keys = keys
	a.keysExpiresAt = a.now().Add(5 * time.Minute)
	return nil
}

func decodePart(encoded string, destination any) error {
	data, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return fmt.Errorf("decode JWT part: %w", err)
	}
	if err := json.Unmarshal(data, destination); err != nil {
		return fmt.Errorf("parse JWT part: %w", err)
	}
	return nil
}

func validRole(role Role) bool {
	return role == RoleAnalyst || role == RoleReviewer || role == RoleAdmin
}

type principalContextKey struct{}

func WithPrincipal(ctx context.Context, principal Principal) context.Context {
	return context.WithValue(ctx, principalContextKey{}, principal)
}

func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalContextKey{}).(Principal)
	return principal, ok
}
