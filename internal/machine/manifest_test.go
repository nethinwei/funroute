package machine_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/nethinwei/funroute/internal/machine"
)

// A manifest crosses from a host to a language service as JSON, so its
// shape is fixed, by digest, and it reads back to itself.
func TestManifestJSONShape(t *testing.T) {
	t.Parallel()
	encoded, err := json.Marshal(snapshotRegistry(t).Manifest())
	if err != nil {
		t.Fatal(err)
	}
	const want = "63c775a4d598d2a4be05be1222d27e84801ea5807acbae255fd93657921dfefd"
	if got := fmt.Sprintf("%x", sha256.Sum256(encoded)); got != want {
		t.Fatalf("sha256(json.Marshal(manifest)) = %s, want %s", got, want)
	}
	var back machine.Manifest
	if err := json.Unmarshal(encoded, &back); err != nil {
		t.Fatal(err)
	}
	if again, _ := json.Marshal(back); string(again) != string(encoded) {
		t.Fatal("a manifest read back writes something else")
	}
}
