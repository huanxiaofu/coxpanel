// Package auth 处理 JWT 签发/校验、密码哈希。
package auth

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

var ErrWeakSigningSecret = errors.New("signing secret is too weak")

// ValidateSigningSecret rejects configuration that is missing enough key
// material for HS256 or contains control characters that could be logged or
// transported ambiguously. Callers must supply the secret; no fallback exists.
func ValidateSigningSecret(secret string) error {
	if len([]byte(secret)) < 32 || strings.ContainsAny(secret, "\r\n\x00") {
		return fmt.Errorf("%w: require at least 32 bytes", ErrWeakSigningSecret)
	}
	return nil
}

// Service 鉴权服务。
type Service struct {
	secret []byte
	ttl    time.Duration
}

// NewService 创建鉴权服务。
func NewService(secret string, ttl time.Duration) *Service {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return &Service{secret: []byte(secret), ttl: ttl}
}

// Claims JWT 载荷。
type Claims struct {
	UserID   int64  `json:"uid"`
	Username string `json:"uname"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

// IssueToken 签发 JWT。
func (s *Service) IssueToken(userID int64, username, role string) (string, error) {
	claims := Claims{
		UserID:   userID,
		Username: username,
		Role:     role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(s.ttl)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return t.SignedString(s.secret)
}

// ParseToken 校验并解析 JWT。
func (s *Service) ParseToken(tokenStr string) (*Claims, error) {
	if s == nil || len(s.secret) == 0 || tokenStr == "" {
		return nil, errors.New("invalid token")
	}
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("unexpected signing method")
		}
		return s.secret, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token")
	}
	if claims.ExpiresAt == nil {
		return nil, errors.New("token expiry is required")
	}
	return claims, nil
}

// HashPassword 生成 bcrypt 哈希。
func HashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(b), err
}

// CheckPassword 校验密码。
func CheckPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}
