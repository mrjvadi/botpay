// Package auth provides JWT token generation/validation and AES-256-GCM encryption
// for storing sensitive values like bot tokens.
package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"io"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ---- JWT ----

// Claims is the JWT payload used for API authentication.
type Claims struct {
	UserID string `json:"uid"`
	Role   string `json:"role"`
	jwt.RegisteredClaims
}

// JWTConfig holds secrets and TTLs for JWT generation.
type JWTConfig struct {
	AccessSecret  string
	RefreshSecret string
	AccessTTL     time.Duration
	RefreshTTL    time.Duration
}

// GenerateAccessToken creates a signed access token.
func GenerateAccessToken(userID, role string, cfg JWTConfig) (string, error) {
	return signToken(userID, role, cfg.AccessSecret, cfg.AccessTTL)
}

// GenerateRefreshToken creates a signed refresh token.
func GenerateRefreshToken(userID, role string, cfg JWTConfig) (string, error) {
	return signToken(userID, role, cfg.RefreshSecret, cfg.RefreshTTL)
}

// ParseAccessToken validates and parses an access token.
func ParseAccessToken(tokenStr, secret string) (*Claims, error) {
	return parseClaims(tokenStr, secret)
}

// ParseRefreshToken validates and parses a refresh token.
func ParseRefreshToken(tokenStr, secret string) (*Claims, error) {
	return parseClaims(tokenStr, secret)
}

func signToken(userID, role, secret string, ttl time.Duration) (string, error) {
	claims := Claims{
		UserID: userID,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}

func parseClaims(tokenStr, secret string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}

// ---- Service-to-service auth (pay.* / NATS request-reply callers) ----
//
// Every NATS request that touches money (protocol.PayRequest) carries a
// ServiceID + ServiceKey pair. Historically the responder side (botpay)
// only ever checked that ServiceID was a known string and NEVER validated
// ServiceKey — meaning anyone who could reach NATS (all services share one
// NATS username/password) could impersonate "botmanager" or any other
// service by simply naming it, with no secret required at all. That let a
// compromised/malicious NATS client mint or drain TON balances at will.
//
// ComputeServiceKey/ValidateServiceKey close that hole: the key is an
// HMAC-SHA256 of the service's own name under a master secret
// (SERVICE_HMAC_SECRET) that only trusted central services hold — never
// distributed to per-customer bot containers. A caller who doesn't have
// the master secret cannot forge a valid key for any service_id.

// ComputeServiceKey derives the auth key a service must present for a given
// serviceID, from the shared master secret. Deterministic — callers compute
// it themselves at startup, no key distribution/rotation bookkeeping needed
// beyond rotating the one master secret.
func ComputeServiceKey(masterSecret, serviceID string) string {
	mac := hmac.New(sha256.New, []byte(masterSecret))
	mac.Write([]byte(serviceID))
	return hex.EncodeToString(mac.Sum(nil))
}

// ValidateServiceKey checks a presented key against the expected HMAC for
// serviceID, using a constant-time comparison to avoid timing side-channels.
func ValidateServiceKey(masterSecret, serviceID, presentedKey string) bool {
	if masterSecret == "" || presentedKey == "" {
		return false
	}
	expected := ComputeServiceKey(masterSecret, serviceID)
	return subtle.ConstantTimeCompare([]byte(expected), []byte(presentedKey)) == 1
}

// ---- AES-256-GCM ----

// Encrypt encrypts plaintext using AES-256-GCM.
// keyHex must be a 64-character hex string (32 bytes).
func Encrypt(plaintext, keyHex string) (string, error) {
	key, err := hex.DecodeString(keyHex)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return hex.EncodeToString(sealed), nil
}

// Decrypt decrypts a ciphertext produced by Encrypt.
func Decrypt(cipherHex, keyHex string) (string, error) {
	key, err := hex.DecodeString(keyHex)
	if err != nil {
		return "", err
	}
	data, err := hex.DecodeString(cipherHex)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(data) < gcm.NonceSize() {
		return "", errors.New("ciphertext too short")
	}
	plain, err := gcm.Open(nil, data[:gcm.NonceSize()], data[gcm.NonceSize():], nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
