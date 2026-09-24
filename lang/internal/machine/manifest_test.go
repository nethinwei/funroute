package machine_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"

	"funroute/lang/internal/machine"
)

// A manifest crosses from a host to a language service as JSON, so its
// shape is fixed, by digest, and it reads back to itself.
func TestManifestJSONShape(t *testing.T) {
	t.Parallel()
	encoded, err := json.Marshal(snapshotRegistry(t).Manifest())
	if err != nil {
		t.Fatal(err)
	}
	const want = "d7dc32198fbccd8b26421692d7a208914f11764bcc649a621f3859b8cba59231"
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
