package websocket

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"lark-daemon/internal/proto"
)

// JWTClaims represents JWT token claims.
type JWTClaims struct {
	MemberID    string `json:"member_id"`
	WorkspaceID string `json:"workspace_id"`
	jwt.RegisteredClaims
}

// TokenBlacklist is the interface for checking revoked tokens.
type TokenBlacklist interface {
	IsTokenBlacklisted(ctx context.Context, jti string) (bool, error)
}

// AuthService handles authentication.
type AuthService struct {
	jwtSecret []byte
}

// NewAuthService creates a new auth service.
func NewAuthService(secret string) *AuthService {
	return &AuthService{
		jwtSecret: []byte(secret),
	}
}

// GenerateToken creates a JWT token for a member with a unique JTI for revocation.
func (a *AuthService) GenerateToken(memberID, workspaceID string) (string, string, error) {
	jtiBytes := make([]byte, 16)
	if _, err := rand.Read(jtiBytes); err != nil {
		return "", "", fmt.Errorf("generate jti: %w", err)
	}
	jti := hex.EncodeToString(jtiBytes)
	claims := JWTClaims{
		MemberID:    memberID,
		WorkspaceID: workspaceID,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        jti,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)), // 24 hours
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "lark",
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(a.jwtSecret)
	return signed, jti, err
}

// ValidateToken validates a JWT token and returns the claims.
// If a blacklist is provided, it also checks whether the token has been revoked.
func (a *AuthService) ValidateToken(tokenStr string, blacklist TokenBlacklist) (*JWTClaims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &JWTClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return a.jwtSecret, nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*JWTClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}
	if blacklist != nil && claims.ID != "" {
		blocked, err := blacklist.IsTokenBlacklisted(context.Background(), claims.ID)
		if err != nil {
			return nil, fmt.Errorf("blacklist check: %w", err)
		}
		if blocked {
			return nil, fmt.Errorf("token revoked")
		}
	}
	return claims, nil
}

// GenerateAPIKey generates a random API key with the given prefix.
func GenerateAPIKey(prefix string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate api key: %w", err)
	}
	return prefix + hex.EncodeToString(b), nil
}

// GenerateProvisionToken generates a provision token for a workspace.
func GenerateProvisionToken() (string, error) {
	return GenerateAPIKey(proto.AgentProvisionTokenPrefix)
}

