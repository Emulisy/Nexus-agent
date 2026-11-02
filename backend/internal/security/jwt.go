// Package security provides JWTs, credential encryption, and input normalization.
// JWTManager issues access and refresh tokens with distinct issuers and random
// IDs. Redis allowlisting/rotation belongs to AuthService; cryptographic token
// parsing alone does not check refresh-token revocation.
package security

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims carries user identity/role alongside the JWT library's registered expiry, issuer, and token-ID fields.
type Claims struct {
	UserID   int64  `json:"uid"`
	Role     string `json:"role"`
	Username string `json:"username"`
	jwt.RegisteredClaims
}

// TokenPair returns signed access/refresh strings and the access lifetime; internal UserID is excluded from JSON.
type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"` // Access token lifetime in seconds.
	UserID       int64  `json:"-"`          // Subject used for the refresh allowlist; excluded from JSON.
}

// JWTManager stores immutable signing bytes and issuance lifetimes shared by authentication requests.
type JWTManager struct {
	secret     []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
}

// NewJWTManager copies configured signing/lifetime values into the issuer; production configuration validates secrets.
func NewJWTManager(secret string, accessTTL, refreshTTL time.Duration) *JWTManager {
	return &JWTManager{secret: []byte(secret), accessTTL: accessTTL, refreshTTL: refreshTTL}
}

// Generate signs an HS256 access token and refresh token with separate expiries/issuers.
// Both receive independent cryptographically random IDs, ensuring repeated
// issuance within one timestamp still produces distinct refresh tokens.
func (m *JWTManager) Generate(userID int64, role, username string) (*TokenPair, error) {
	now := time.Now()
	accessID, err := newTokenID()
	if err != nil {
		return nil, fmt.Errorf("generate access token id: %w", err)
	}
	refreshID, err := newTokenID()
	if err != nil {
		return nil, fmt.Errorf("generate refresh token id: %w", err)
	}
	access := jwt.NewWithClaims(jwt.SigningMethodHS256, &Claims{
		UserID: userID, Role: role, Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(m.accessTTL)),
			IssuedAt:  jwt.NewNumericDate(now),
			Issuer:    "nexus-agent",
			ID:        accessID,
		},
	})
	accessStr, err := access.SignedString(m.secret)
	if err != nil {
		return nil, fmt.Errorf("sign access token: %w", err)
	}

	refresh := jwt.NewWithClaims(jwt.SigningMethodHS256, &Claims{
		UserID: userID, Role: role, Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(m.refreshTTL)),
			IssuedAt:  jwt.NewNumericDate(now),
			Issuer:    "nexus-agent:refresh",
			ID:        refreshID,
		},
	})
	refreshStr, err := refresh.SignedString(m.secret)
	if err != nil {
		return nil, fmt.Errorf("sign refresh token: %w", err)
	}

	return &TokenPair{AccessToken: accessStr, RefreshToken: refreshStr, ExpiresIn: int64(m.accessTTL.Seconds()), UserID: userID}, nil
}

// newTokenID encodes 16 random bytes as hex for the JWT ID claim and propagates entropy-source errors.
func newTokenID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Parse verifies a HMAC-signed token using registered-claim validation and the expected issuer.
// Expired-token errors remain recognizable through errors.Is. isRefresh selects
// the refresh issuer; allowlist membership is checked separately by AuthService.
func (m *JWTManager) Parse(tokenStr string, isRefresh bool) (*Claims, error) {
	claims := &Claims{}
	_, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return m.secret, nil
	})
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, err
		}
		return nil, fmt.Errorf("parse token: %w", err)
	}
	wantIssuer := "nexus-agent"
	if isRefresh {
		wantIssuer = "nexus-agent:refresh"
	}
	if claims.Issuer != wantIssuer {
		return nil, fmt.Errorf("token type mismatch")
	}
	return claims, nil
}

// RefreshTTL exposes the configured lifetime used for Redis refresh-token set expiration.
func (m *JWTManager) RefreshTTL() time.Duration { return m.refreshTTL }
