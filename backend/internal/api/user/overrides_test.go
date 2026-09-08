package user

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coxpanel/backend/internal/repo"
)

func TestOverrideViewRedactsSecretAndPreservesZero(t *testing.T) {
	row := repo.OverrideRow{SortOrderSet: true, Params: json.RawMessage(`{"obfsPassword":"test-only-secret","sni":"example.test"}`)}
	encoded, _ := json.Marshal(overrideView(row))
	if strings.Contains(string(encoded), "test-only-secret") || !strings.Contains(string(encoded), `"sortOrder":0`) || !strings.Contains(string(encoded), "configured") {
		t.Fatal("override view must redact secret and preserve explicit zero")
	}
}

func TestDecodeOverrideRejectsUnknownAndTrailing(t *testing.T) {
	for _, body := range []string{`{"uuid":"forbidden"}`, `{} {}`, `{"params":{},"unexpected":1}`} {
		request := httptest.NewRequest("PUT", "/", strings.NewReader(body))
		var value overrideWrite
		if decodeJSON(request, &value) == nil {
			t.Fatal("invalid body accepted")
		}
	}
}
