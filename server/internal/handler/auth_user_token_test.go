package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/JanMori/Orchestra/server/pkg/db/generated"
)

func TestGetTaskUserToken_Success(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	userUUID := parseUUID(testUserID)

	// Set test token
	_, err := testHandler.Queries.UpdateUserAccessToken(ctx, db.UpdateUserAccessTokenParams{
		ID:          userUUID,
		AccessToken: pgtype.Text{String: "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.test_token", Valid: true},
	})
	if err != nil {
		t.Fatalf("failed to update user access token: %v", err)
	}

	t.Cleanup(func() {
		testHandler.Queries.UpdateUserAccessToken(ctx, db.UpdateUserAccessTokenParams{
			ID:          userUUID,
			AccessToken: pgtype.Text{Valid: false},
		})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/auth/user-token", nil)
	req.Header.Set("X-User-ID", testUserID)
	rec := httptest.NewRecorder()

	testHandler.GetTaskUserToken(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp UserTokenResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Token != "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.test_token" {
		t.Errorf("expected token %q, got %q", "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.test_token", resp.Token)
	}
}

func TestGetTaskUserToken_NotFound(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	userUUID := parseUUID(testUserID)

	// Clear token
	_, err := testHandler.Queries.UpdateUserAccessToken(ctx, db.UpdateUserAccessTokenParams{
		ID:          userUUID,
		AccessToken: pgtype.Text{Valid: false},
	})
	if err != nil {
		t.Fatalf("failed to clear user access token: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/auth/user-token", nil)
	req.Header.Set("X-User-ID", testUserID)
	rec := httptest.NewRecorder()

	testHandler.GetTaskUserToken(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetTaskUserToken_Unauthorized(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/auth/user-token", nil)
	rec := httptest.NewRecorder()

	testHandler.GetTaskUserToken(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d: %s", rec.Code, rec.Body.String())
	}
}
