package web

import (
	"os"
	"testing"
)

// The tests of this package look for the Vietnamese text, which was the default language when most of them were written. Text made
// outside a request is drawn in Vietnamese here, and do() asks for Vietnamese the way a browser does.
func TestMain(m *testing.M) {
	globalLanguage = "vi"
	os.Exit(m.Run())
}
