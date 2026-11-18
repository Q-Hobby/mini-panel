package gdrive

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func putFile(f *fake, id string, n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i*7 + i/251)
	}
	f.content[id] = b
	return b
}

func dest(t *testing.T) string { return filepath.Join(t.TempDir(), "files.tar.gz") }

// Windows reports every file as 0666, so the mode can only be checked where the program really runs.
func runtimeNotWindows() bool { return runtime.GOOS != "windows" }

func TestDownloadWholeFile(t *testing.T) {
	f := newFake(t)
	want := putFile(f, "f1", 300<<10+17)
	p := dest(t)
	if err := f.client().Download(context.Background(), "f1", p, int64(len(want))); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(p); !bytes.Equal(got, want) {
		t.Error("the file on disk is not the file on the drive")
	}
	if f.ranges != 0 {
		t.Errorf("an unbroken download needs no ranged request, got %d", f.ranges)
	}
	if runtimeNotWindows() {
		if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
			t.Errorf("the copy of a backup must be private, mode %v", fi.Mode().Perm())
		}
	}
}

func TestDownloadGoesOnFromWhereTheConnectionBroke(t *testing.T) {
	f := newFake(t)
	want := putFile(f, "f1", 500<<10)
	f.cutAfter = 123456
	p := dest(t)
	if err := f.client().Download(context.Background(), "f1", p, int64(len(want))); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(p); !bytes.Equal(got, want) {
		t.Error("the file must be whole after a break")
	}
	if f.ranges != 1 {
		t.Errorf("the second try must ask only for the rest, ranged requests: %d", f.ranges)
	}
}

func TestDownloadStartsOverWhenGoogleIgnoresTheRange(t *testing.T) {
	f := newFake(t)
	want := putFile(f, "f1", 400<<10)
	f.cutAfter, f.ignoreRange = 100000, true
	p := dest(t)
	if err := f.client().Download(context.Background(), "f1", p, int64(len(want))); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(p); !bytes.Equal(got, want) {
		t.Error("a whole file sent for a ranged request must replace what was there, not be added to it")
	}
}

func TestDownloadAsksForANewTokenWhenTheOldOneIsRefused(t *testing.T) {
	f := newFake(t)
	want := putFile(f, "f1", 1000)
	f.badTokens = 1
	p := dest(t)
	if err := f.client().Download(context.Background(), "f1", p, 1000); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(p); !bytes.Equal(got, want) || f.tokens < 2 {
		t.Errorf("tokens asked for: %d", f.tokens)
	}
}

func TestDownloadOfAFileThatIsGoneFailsAtOnce(t *testing.T) {
	f := newFake(t)
	err := f.client().Download(context.Background(), "nope", dest(t), 10)
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != 404 {
		t.Errorf("got %v", err)
	}
}

func TestDownloadThatEndsShortGivesUp(t *testing.T) {
	f := newFake(t)
	putFile(f, "f1", 1000)
	if err := f.client().Download(context.Background(), "f1", dest(t), 5000); err == nil {
		t.Error("a file that is smaller than Google said must fail, not be accepted")
	}
}

func TestDownloadOfAnEmptyFileAndOfAnIdWithOddCharacters(t *testing.T) {
	f := newFake(t)
	p := dest(t)
	if err := f.client().Download(context.Background(), "whatever", p, 0); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(p); err != nil || fi.Size() != 0 {
		t.Errorf("an empty file is made: %v", err)
	}
	if err := f.client().Download(context.Background(), "../../etc/passwd", dest(t), 10); err == nil {
		t.Error("an id cannot reach another path")
	}
	if err := f.client().Download(context.Background(), "x", dest(t), -1); err == nil {
		t.Error("a size that is not known must be refused")
	}
}

func TestDownloadStopsWhenTheContextIsCancelled(t *testing.T) {
	f := newFake(t)
	putFile(f, "f1", 1000)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := f.client().Download(ctx, "f1", dest(t), 1000); err == nil {
		t.Error("a cancelled download must stop")
	}
}
