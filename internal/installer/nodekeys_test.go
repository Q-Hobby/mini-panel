package installer

import (
	"strings"
	"testing"
)

// The script of a Node.js install checks the signature of SHASUMS256.txt with the keys built into mpn before it trusts any line of
// it, and stops when gpgv says no.
func TestNodeIsCheckedAgainstTheBuiltInKeys(t *testing.T) {
	if len(nodeReleaseKeys) < 1000 {
		t.Fatalf("the keyring is %d bytes", len(nodeReleaseKeys))
	}
	s, err := nodeStep("22")
	if err != nil {
		t.Fatal(err)
	}
	var script string
	for _, a := range s.Actions {
		if strings.Contains(strings.Join(a.Args, " "), "SHASUMS256.txt") {
			script = strings.Join(a.Args, " ")
		}
	}
	for _, want := range []string{"SHASUMS256.txt.sig", "gpgv --keyring", NodeKeyringFile, "|| { echo", "sha256sum -c -"} {
		if !strings.Contains(script, want) {
			t.Errorf("the script lacks %q", want)
		}
	}
	if strings.Index(script, "gpgv") > strings.Index(script, "sha256sum -c") {
		t.Error("the sums are trusted before their signature is checked")
	}
	var wrote bool
	for _, a := range s.Actions {
		if a.Path == NodeKeyringFile && a.Content == string(nodeReleaseKeys) {
			wrote = true
		}
	}
	if !wrote {
		t.Error("the step does not write the built-in keyring")
	}
}
