package qr

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

// Before github.com/skip2/go-qrcode was removed, this package was checked module by module against it for every byte-mode text from
// 1 to 213 bytes (all ten versions) with the same mask. These digests keep the result from changing unnoticed since.
var goldenTexts = []string{
	"otpauth://totp/Mini%20Panel:admin?secret=JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP&issuer=Mini+Panel&algorithm=SHA1&digits=6&period=30",
	"A",
	strings.Repeat("x", 100),
	strings.Repeat("0123456789", 21),
}

var goldenDigests = []struct {
	size   int
	digest string
}{
	{49, "ae45fb6b869d9979"},
	{21, "f5c1d2ce63a03b40"},
	{41, "a6be9b98b4f3c5d3"},
	{57, "2f398b9367f314dc"},
}

func digest(m [][]bool) string {
	h := sha256.New()
	for _, row := range m {
		for _, on := range row {
			if on {
				h.Write([]byte{1})
			} else {
				h.Write([]byte{0})
			}
		}
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func TestTheSymbolsAreTheOnesThatWereChecked(t *testing.T) {
	for i, text := range goldenTexts {
		if i >= len(goldenDigests) {
			break
		}
		m, err := Encode(text)
		if err != nil {
			t.Fatal(err)
		}
		if len(m) != goldenDigests[i].size || digest(m) != goldenDigests[i].digest {
			t.Errorf("text %d: size %d digest %s, want %d %s", i, len(m), digest(m), goldenDigests[i].size, goldenDigests[i].digest)
		}
	}
}

func TestTheSmallestSymbolIsChosenAndTooLongIsRefused(t *testing.T) {
	for _, c := range []struct{ n, size int }{{1, 21}, {14, 21}, {15, 25}, {122, 45}, {123, 49}, {MaxBytes, 57}} {
		m, err := Encode(strings.Repeat("a", c.n))
		if err != nil || len(m) != c.size {
			t.Errorf("%d bytes: size %d err %v, want %d", c.n, len(m), err, c.size)
		}
	}
	if _, err := Encode(strings.Repeat("a", MaxBytes+1)); err == nil {
		t.Error("a text longer than version 10 holds was encoded")
	}
}

func TestTheFinderPatternsAreInTheirCorners(t *testing.T) {
	m, _ := Encode("hello")
	n := len(m)
	for _, c := range [][2]int{{0, 0}, {n - 7, 0}, {0, n - 7}} {
		for d := 0; d < 7; d++ {
			if !m[c[1]][c[0]+d] || !m[c[1]+6][c[0]+d] || !m[c[1]+d][c[0]] || !m[c[1]+d][c[0]+6] {
				t.Fatalf("finder at %v is not whole", c)
			}
		}
		if m[c[1]+1][c[0]+1] || !m[c[1]+3][c[0]+3] {
			t.Errorf("finder at %v has the wrong inside", c)
		}
	}
}
