package installer

import (
	"strings"
	"testing"
)

// While a change of the panel's port waits, nginx answers on both; the new port is the first, which is the one mpn reads afterwards.
func TestThePanelCanListenOnTwoPorts(t *testing.T) {
	c := PanelNginx(8443, 2610)
	i, j := strings.Index(c, "listen 8443 ssl http2;"), strings.Index(c, "listen 2610 ssl http2;")
	if i < 0 || j < 0 || i > j || !strings.Contains(c, "listen [::]:8443 ssl http2;") || !strings.Contains(c, "listen [::]:2610 ssl http2;") {
		t.Errorf("two ports:\n%s", c)
	}
	if panelNginx(2610) != PanelNginx(2610) {
		t.Error("one port must give the same file as before")
	}
}

func TestTheBrowserKeepsAPortOfItsOwn(t *testing.T) {
	c := PMANginx(8443, 2711)
	if !strings.Contains(c, "listen 2711 ssl http2;") || !strings.Contains(c, "return 302 https://$host:8443/login;") {
		t.Errorf("pma on its own port:\n%s", c)
	}
	if pmaPortFor(Options{}, 2610) != 2611 || pmaPortFor(Options{PMAPort: 2711}, 2610) != 2711 {
		t.Error("pmaPortFor")
	}
}

// EL labelled 2612 for the web mail whatever the panel's port was.
func TestSELinuxLabelsTheWebmailPortOfThisPanel(t *testing.T) {
	got := elPanelPorts(Options{PanelPort: 8443})
	if len(got) != 3 || got[0] != 8443 || got[1] != 8444 || got[2] != 8445 {
		t.Errorf("elPanelPorts = %v", got)
	}
}
