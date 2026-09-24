package codexappserver

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
)

func TestVersionedSchemaFixtureMatchesPinnedDigest(t *testing.T) {
	data, err := os.ReadFile(SchemaPathV0154)
	if err != nil {
		t.Fatalf("read schema fixture: %v", err)
	}
	var schema map[string]json.RawMessage
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatalf("decode schema fixture: %v", err)
	}
	digest := sha256.Sum256(data)
	if got := hex.EncodeToString(digest[:]); got != SchemaSHA256V0154 {
		t.Fatalf("schema digest = %s, want %s", got, SchemaSHA256V0154)
	}
	if _, ok := schema["definitions"]; !ok {
		t.Fatal("schema fixture has no protocol definitions")
	}
}
