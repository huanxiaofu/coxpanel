package repo

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestP2SensitivePreviewRequiresSnapshotSealer(t *testing.T) {
	repository := NewTopologyRepo(nil)
	err := repository.SavePreviewV2(context.Background(), &TopologyPreviewV2{
		PreviewID:  "preview-1",
		RootNodeID: 1,
		ExpiresAt: time.Now().Add(10 * time.Minute),
		Candidates: []TopologyCandidate{{NodeID: 1, Rendered: []byte(`{"inbounds":[],"outbounds":[]}`)}},
	})
	if !errors.Is(err, ErrTopologySecureStorageUnavailable) {
		t.Fatalf("SavePreviewV2() error = %v, want %v", err, ErrTopologySecureStorageUnavailable)
	}
}
