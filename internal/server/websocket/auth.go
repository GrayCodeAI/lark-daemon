package websocket

import (
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

// GenerateToken creates a JWT token for a member.
func (a *AuthService) GenerateToken(memberID, workspaceID string) (string, error) {
	claims := JWTClaims{
		MemberID:    memberID,
		WorkspaceID: workspaceID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(7 * 24 * time.Hour)), // 7 days
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "lark",
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(a.jwtSecret)
}

// ValidateToken validates a JWT token and returns the claims.
func (a *AuthService) ValidateToken(tokenStr string) (*JWTClaims, error) {
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
	return claims, nil
}

// GenerateAPIKey generates a random API key with the given prefix.
func GenerateAPIKey(prefix string) string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand.Read failed: " + err.Error())
	}
	return prefix + hex.EncodeToString(b)
}

// GenerateProvisionToken generates a provision token for a workspace.
func GenerateProvisionToken() string {
	return GenerateAPIKey(proto.AgentProvisionTokenPrefix)
}

