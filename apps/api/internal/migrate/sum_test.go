package migrate

import "testing"

func TestEmbeddedChecksums(t *testing.T) {
	if err := verifyEmbeddedChecksums(); err != nil {
		t.Fatal(err)
	}
}
