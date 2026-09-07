package generator

import (
	"encoding/json"
	"testing"

	"github.com/coxpanel/backend/internal/models"
	"gopkg.in/yaml.v3"
)

func TestGenerateMihomoOriginalAPIYAMLRed(t *testing.T) {
	const displayName = "x: [a]\nb: c"
	body, err := GenerateMihomo([]Proxy{{
		Name:     "base",
		Override: &OverrideData{DisplayName: displayName},
		Node: &models.Node{
			Type:        "external",
			ExtProtocol: "shadowsocks",
			ExtParams:   json.RawMessage(`{"server":"127.0.0.1","port":"443","password":"secret"}`),
		},
	}}, "subscription")
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := yaml.Unmarshal(body, &document); err != nil {
		t.Fatalf("generated YAML does not parse: %v\n%s", err, body)
	}
}
