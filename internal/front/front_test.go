package front

import (
	"strings"
	"testing"

	"mpn/internal/installer"
	"mpn/internal/mail"
	"mpn/internal/panelssl"
	"mpn/internal/vhost"
)

func TestNothingIsServedOnPort443WithoutAName(t *testing.T) {
	files, err := Files(Input{})
	if err != nil || len(files) != 0 {
		t.Errorf("no name, no block: %v %v", files, err)
	}
	// the block that answers Let's Encrypt is left as it is while the name has no block of its own
	files, _ = Files(Input{PanelACME: true, PMA: true, Webmail: true})
	if len(files) != 0 {
		t.Errorf("%v", files)
	}
}

func TestThePanelsNameGetsABlockAndTheRedirectOfPort80Follows(t *testing.T) {
	files, err := Files(Input{Domain: "c.demo.com", PMA: true, PanelACME: true})
	if err != nil {
		t.Fatal(err)
	}
	conf := files[installer.PanelDomainFile]
	if !strings.Contains(conf, "server_name c.demo.com;") || !strings.Contains(conf, "location ^~ /pma/ {") {
		t.Errorf("the panel's block:\n%s", conf)
	}
	if acme := files[panelssl.ACMEConf]; !strings.Contains(acme, "return 301 https://$host$request_uri;") {
		t.Errorf("port 80 must lead to the plain address:\n%s", acme)
	}
	if strings.Contains(conf, "/webmail/") {
		t.Error("no web mail location while the web mail is off")
	}
	noPMA, _ := Files(Input{Domain: "c.demo.com"})
	if strings.Contains(noPMA[installer.PanelDomainFile], "/pma/") {
		t.Error("without the database browser there is nothing under /pma/")
	}
}

func TestTheWebMailIsUnderWebmailOnTheSameNameAsThePanelAndPma(t *testing.T) {
	files, err := Files(Input{Domain: "c.demo.com", PMA: true, Webmail: true})
	if err != nil {
		t.Fatal(err)
	}
	conf := files[installer.PanelDomainFile]
	for _, want := range []string{"location ^~ /pma/ {", "location ^~ /webmail/ {", "location = /webmail { return 301 /webmail/; }",
		"proxy_pass http://127.0.0.1:8781/;", "proxy_set_header X-Forwarded-Prefix /webmail;", "proxy_set_header Host $http_host;"} {
		if !strings.Contains(conf, want) {
			t.Errorf("lacks %q:\n%s", want, conf)
		}
	}
	// the panel's own location comes after and still catches everything else
	if strings.Index(conf, "location / {") < strings.Index(conf, "location ^~ /webmail/ {") {
		t.Error("the root location must not shadow /webmail/")
	}
	// the headers that narrow what a flaw on this shared name could reach
	for _, want := range []string{"Cross-Origin-Opener-Policy same-origin", "connect-src https://$http_host/webmail/;", "form-action https://$http_host/webmail/;"} {
		if !strings.Contains(conf, want) {
			t.Errorf("lacks %q", want)
		}
	}
}

func TestANameThatIsNotAValidHostIsRefused(t *testing.T) {
	for _, in := range []Input{{Domain: "c.demo.com; }"}, {Domain: "localhost"}, {Domain: "UPPER.demo.com"}} {
		if _, err := Files(in); err == nil {
			t.Errorf("%+v must be refused: the name goes into nginx's configuration", in)
		}
	}
}

func TestManagedListsEveryFileFilesCanReturnExceptPort80(t *testing.T) {
	files, _ := Files(Input{Domain: "c.demo.com", Webmail: true, PMA: true, PanelACME: true})
	for p := range files {
		if p == panelssl.ACMEConf {
			continue // removed with the certificate, not by sync
		}
		found := false
		for _, m := range Managed {
			found = found || m == p
		}
		if !found {
			t.Errorf("%s is written but would never be removed", p)
		}
	}
}

// vhost cannot import mail (and mail not vhost), so the two constants are kept equal here.
func TestTheWebMailPortIsTheSameInNginxAndInTheProgram(t *testing.T) {
	if vhost.WebmailBackendPort != mail.WebmailBackendPort {
		t.Errorf("vhost says %d, mail says %d", vhost.WebmailBackendPort, mail.WebmailBackendPort)
	}
}
