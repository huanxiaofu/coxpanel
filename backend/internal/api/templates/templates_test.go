package templates

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTemplateRequestsRejectTrailingAndUnknownFields(t *testing.T) {
	for _, body := range []string{`{} {}`, `{"secret":"forbidden"}`, `{"definition":{},"unexpected":1}`} {
		request := httptest.NewRequest("POST", "/", strings.NewReader(body))
		if _, err := decodeTemplateRequest(httptest.NewRecorder(), request); err == nil {
			t.Fatal("invalid template request accepted")
		}
	}
}

func TestTemplateMutationsRequireOwner(t *testing.T) {
	request := httptest.NewRequest("POST", "/", strings.NewReader(`{}`))
	response := httptest.NewRecorder()
	if requireOwner(response, request) || response.Code != 403 {
		t.Fatal("missing owner accepted")
	}
}
