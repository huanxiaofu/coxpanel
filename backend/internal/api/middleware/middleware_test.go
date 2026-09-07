package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coxpanel/backend/internal/auth"
	"github.com/coxpanel/backend/internal/models"
	"github.com/golang-jwt/jwt/v5"
)

type fakeCurrentUserStore struct {
	user *models.User
}

func (f *fakeCurrentUserStore) GetUser(_ context.Context, _ int64) (*models.User, error) {
	return f.user, nil
}

func TestRequireAuthUsesCurrentUserStateAndRole(t *testing.T) {
	service := auth.NewService("synthetic-middleware-secret", time.Hour)
	handler := RequireAuth(service, &fakeCurrentUserStore{user: &models.User{ID: 7, Active: true, Role: "user"}})(
		RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})),
	)
	token, err := service.IssueToken(7, "stale-role", "owner")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d when DB role differs from JWT", resp.Code, http.StatusForbidden)
	}
}

func TestRequireAuthRejectsInactiveCurrentUser(t *testing.T) {
	service := auth.NewService("synthetic-middleware-secret", time.Hour)
	handler := RequireAuth(service, &fakeCurrentUserStore{user: &models.User{ID: 7, Active: false, Role: "owner"}})(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
	)
	token, err := service.IssueToken(7, "inactive", "owner")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d for inactive user", resp.Code, http.StatusUnauthorized)
	}
}

func TestRequireAuthRejectsExpiredJWT(t *testing.T) {
	service := auth.NewService("synthetic-middleware-secret", time.Hour)
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.Claims{
		UserID:   7,
		Username: "expired",
		Role:     "owner",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Minute)),
		},
	})
	raw, err := token.SignedString([]byte("synthetic-middleware-secret"))
	if err != nil {
		t.Fatal(err)
	}
	handler := RequireAuth(service, &fakeCurrentUserStore{user: &models.User{ID: 7, Active: true, Role: "owner"}})(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
	)
	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d for expired JWT", resp.Code, http.StatusUnauthorized)
	}
}

func TestRequireAuthRejectsExpiredCurrentUser(t *testing.T) {
	service := auth.NewService("synthetic-middleware-secret", time.Hour)
	expired := time.Now().Add(-time.Minute)
	handler := RequireAuth(service, &fakeCurrentUserStore{user: &models.User{ID: 7, Active: true, Role: "owner", ExpireAt: &expired}})(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
	)
	token, err := service.IssueToken(7, "expired-user", "owner")
	if err != nil {
		t.Fatal("issuing synthetic token failed")
	}
	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d for expired current user", resp.Code, http.StatusUnauthorized)
	}
}
