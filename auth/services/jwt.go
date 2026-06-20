package services

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type accessClaims struct {
	UserID string `json:"uid"`
	jwt.RegisteredClaims
}

func createAccessToken(secret, userID string, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := accessClaims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}

func parseAccessToken(secret, token string) (*accessClaims, error) {
	claims := &accessClaims{}
	parsed, err := jwt.ParseWithClaims(token, claims, hmacKeyFunc(secret))
	if err != nil {
		return nil, err
	}
	if !parsed.Valid {
		return nil, errors.New("invalid access token")
	}
	return claims, nil
}

func hmacKeyFunc(secret string) jwt.Keyfunc {
	return func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(secret), nil
	}
}