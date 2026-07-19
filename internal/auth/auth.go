package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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
	Subject   string `json:"sub"`
	Role      Role   `json:"role"`
	Issuer    string `json:"iss"`
	ExpiresAt int64  `json:"exp"`
	NotBefore int64  `json:"nbf,omitempty"`
}

type header struct {
	Algorithm string `json:"alg"`
	Type      string `json:"typ"`
}

type Authenticator struct {
	secret []byte
	issuer string
	now    func() time.Time
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
	if err := decodePart(parts[0], &tokenHeader); err != nil || tokenHeader.Algorithm != "HS256" || (tokenHeader.Type != "" && tokenHeader.Type != "JWT") {
		return Principal{}, ErrInvalidToken
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return Principal{}, ErrInvalidToken
	}
	mac := hmac.New(sha256.New, a.secret)
	_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return Principal{}, ErrInvalidToken
	}

	var claims claims
	if err := decodePart(parts[1], &claims); err != nil {
		return Principal{}, ErrInvalidToken
	}
	now := a.now().Unix()
	if strings.TrimSpace(claims.Subject) == "" || claims.Issuer != a.issuer || claims.ExpiresAt <= now || claims.NotBefore > now || !validRole(claims.Role) {
		return Principal{}, ErrInvalidToken
	}
	return Principal{Subject: claims.Subject, Role: claims.Role}, nil
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
