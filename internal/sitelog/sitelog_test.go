package sitelog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSelectKeepsTheLastLinesThatMatch(t *testing.T) {
	text := "a 200\nb 404\nc 200\nd 404\ne 404\n"
	lines, matched := Select(text, 2, "404")
	if matched != 3 || len(lines) != 2 || lines[0] != "d 404" || lines[1] != "e 404" {
		t.Errorf("got %v matched %d", lines, matched)
	}
	if lines, matched = Select(text, 10, ""); matched != 5 || len(lines) != 5 {
		t.Errorf("without a search every line counts: %v %d", lines, matched)
	}
	if lines, _ = Select("", 10, ""); len(lines) != 0 {
		t.Errorf("an empty log has no lines: %v", lines)
	}
}

func TestSelectMakesALineHarmless(t *testing.T) {
	long := strings.Repeat("x", 5000)
	lines, _ := Select("ok \x1b[31mred\x07\n"+long+"\n", 10, "")
	if strings.ContainsAny(lines[0], "\x1b\x07") {
		t.Errorf("control characters must not reach the page: %q", lines[0])
	}
	if len(lines[1]) > maxLine+8 || !strings.HasSuffix(lines[1], " ...") {
		t.Errorf("a long line is cut: %d", len(lines[1]))
	}
}

func TestTheValuesAreChecked(t *testing.T) {
	for _, k := range []string{"access", "error", "app"} {
		if ValidKind(k) != nil {
			t.Errorf("%s is a kind", k)
		}
	}
	for _, k := range []string{"", "../etc/passwd", "Access", "access.log"} {
		if ValidKind(k) == nil {
			t.Errorf("%q must be refused: the kind decides the file", k)
		}
	}
	if n, err := ValidLines(0); err != nil || n != DefaultLines {
		t.Error("0 means the default")
	}
	for _, n := range []int{-1, MaxLines + 1} {
		if _, err := ValidLines(n); err == nil {
			t.Errorf("%d lines must be refused", n)
		}
	}
	if ValidQuery(strings.Repeat("a", MaxQuery+1)) == nil || ValidQuery("a\nb") == nil || ValidQuery("a\x00b") == nil {
		t.Error("a long text and a control character must be refused")
	}
	if ValidQuery("GET /wp-login.php 404") != nil || ValidQuery("đường dẫn") != nil {
		t.Error("an ordinary text is fine")
	}
}

func TestTailReadsTheEndAndRefusesALink(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "access.log")
	if _, _, exists, err := Tail(p); err != nil || exists {
		t.Errorf("a missing file is not an error: %v %v", exists, err)
	}
	if err := os.WriteFile(p, []byte("one\ntwo\nthree\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	data, size, exists, err := Tail(p)
	if err != nil || !exists || size != 14 || string(data) != "one\ntwo\nthree\n" {
		t.Errorf("%q %d %v %v", data, size, exists, err)
	}
	// A file longer than the part that is looked at: the first line is cut and must not be shown.
	big := strings.Repeat("0123456789\n", (maxTail/11)+5)
	if err := os.WriteFile(p, []byte(big), 0o600); err != nil {
		t.Fatal(err)
	}
	data, size, _, _ = Tail(p)
	if int64(len(data)) >= size || !strings.HasPrefix(string(data), "0123456789\n") || len(data) > maxTail {
		t.Errorf("cut at a line start: %d of %d", len(data), size)
	}
	link := filepath.Join(dir, "error.log")
	if err := os.Symlink(p, link); err == nil {
		if _, _, _, err := Tail(link); err == nil {
			t.Error("a link in the log folder must not be followed")
		}
	}
}
