package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/GeneJie199/ai-dev-cycle-manager/internal/models"
)

func TestProviderSnapshotKeepsCredentialPrivateAndHeadersImmutable(t *testing.T) {
	const credential = "synthetic-snapshot-credential"
	config := models.AIProviderConfig{ID: "snapshot-fixture", Name: "Snapshot fixture", Kind: models.AIProviderCustomOpenAI, BaseURL: "http://127.0.0.1:12345", Model: "fixture-model", APIKeyHeader: "Authorization", SecretRef: "memory:fixture", Headers: map[string]string{"X-Request-Mode": "original"}, Enabled: true}
	snapshot, err := CaptureProviderSnapshot(context.Background(), config, &secretStoreStub{values: map[string]string{config.SecretRef: credential}})
	if err != nil {
		t.Fatal(err)
	}
	config.Headers["X-Request-Mode"] = "mutated"
	copy := snapshot.Config()
	copy.Headers["X-Request-Mode"] = "mutated-copy"
	if snapshot.Config().Headers["X-Request-Mode"] != "original" {
		t.Fatal("caller mutated a captured request header")
	}
	raw, err := json.Marshal(snapshot)
	if err != nil || strings.Contains(string(raw)+fmt.Sprintf("%v %+v %#v", snapshot, snapshot, snapshot), credential) {
		t.Fatal("opaque snapshot exposed credential through serialization or formatting")
	}
}
