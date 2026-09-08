package repo

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/coxpanel/backend/internal/models"
)

func TestSubscriptionCarriesTemplateVersionAndRevision(t *testing.T) {
	version := 4
	subscription := models.Subscription{
		ID:              7,
		TemplateID:      int64PtrForTest(11),
		TemplateVersion: &version,
		Revision:        9,
	}
	raw, err := json.Marshal(subscription)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["templateVersion"] != float64(4) {
		t.Fatalf("templateVersion = %#v, want 4", decoded["templateVersion"])
	}
	if decoded["revision"] != float64(9) {
		t.Fatalf("revision = %#v, want 9", decoded["revision"])
	}
}

func TestValidateSubscriptionSelectionRejectsUnsupportedOrInconsistentValues(t *testing.T) {
	tests := []struct {
		name        string
		format      string
		templateID  *int64
		templateVer *int
		wantErr     error
	}{
		{name: "unsupported format", format: "v2rayn", wantErr: ErrFormatUnsupported},
		{name: "version without template", format: "mihomo", templateVer: intPtrForTest(1), wantErr: ErrTemplateVersionWithoutTemplate},
		{name: "blank format", format: "", wantErr: ErrFormatUnsupported},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateSubscriptionSelection(test.format, test.templateID, test.templateVer)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestValidateSubscriptionSelectionAcceptsBuiltinAndSupportedFormats(t *testing.T) {
	for _, format := range []string{"mihomo", "sing-box", "base64"} {
		if err := ValidateSubscriptionSelection(format, nil, nil); err != nil {
			t.Fatalf("format %q rejected: %v", format, err)
		}
	}
	templateID := int64(3)
	if err := ValidateSubscriptionSelection("mihomo", &templateID, nil); err != nil {
		t.Fatalf("template following latest rejected: %v", err)
	}
	version := 2
	if err := ValidateSubscriptionSelection("sing-box", &templateID, &version); err != nil {
		t.Fatalf("fixed template version rejected: %v", err)
	}
}

func TestOverridePatchPreservesNullAsInheritance(t *testing.T) {
	patch := OverridePatch{SortOrder: nil}
	if patch.SortOrder != nil {
		t.Fatal("nil sort order must remain an inherited value")
	}
	if got := patch.SortOrderValue(); got != nil {
		t.Fatalf("sort order value = %#v, want nil", got)
	}

	sortOrder := 0
	patch.SortOrder = &sortOrder
	if got := patch.SortOrderValue(); got == nil || *got != 0 {
		t.Fatalf("sort order value = %#v, want explicit zero", got)
	}
}

func int64PtrForTest(value int64) *int64 { return &value }

func intPtrForTest(value int) *int { return &value }
