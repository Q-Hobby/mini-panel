package certs

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"
)

var now = time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)

type authority struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pem  []byte
}

func newKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func serial() *big.Int { n, _ := rand.Int(rand.Reader, big.NewInt(1<<60)); return n }

// issue makes a certificate signed by parent (or self-signed when parent is nil).
func issue(t *testing.T, cn string, ca bool, names []string, parent *authority, notBefore, notAfter time.Time) (authority, []byte) {
	t.Helper()
	key := newKey(t)
	tpl := &x509.Certificate{SerialNumber: serial(), Subject: pkix.Name{CommonName: cn}, DNSNames: names, NotBefore: notBefore, NotAfter: notAfter,
		BasicConstraintsValid: true, IsCA: ca, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	if ca {
		tpl.KeyUsage = x509.KeyUsageCertSign | x509.KeyUsageCRLSign
	}
	signer, signKey := tpl, crypto.Signer(key)
	if parent != nil {
		signer, signKey = parent.cert, parent.key
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, signer, &key.PublicKey, signKey)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := x509.ParseCertificate(der)
	kd, _ := x509.MarshalPKCS8PrivateKey(key)
	return authority{c, key, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})}, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: kd})
}

func pool(a ...authority) *x509.CertPool {
	p := x509.NewCertPool()
	for _, x := range a {
		p.AddCert(x.cert)
	}
	return p
}

func TestACertificateWithItsChainIsAcceptedAndOrdered(t *testing.T) {
	root, _ := issue(t, "Test Root", true, nil, nil, now.Add(-time.Hour*24*365), now.Add(time.Hour*24*3650))
	mid, _ := issue(t, "Test Intermediate", true, nil, &root, now.Add(-time.Hour*24*300), now.Add(time.Hour*24*3000))
	leaf, key := issue(t, "example.com", false, []string{"example.com", "www.example.com"}, &mid, now.Add(-time.Hour), now.Add(time.Hour*24*90))

	// pasted the wrong way round: chain first, and the root included
	chain := append(append([]byte{}, root.pem...), mid.pem...)
	res, err := Check(key, leaf.pem, chain, []string{"example.com", "www.example.com"}, now, pool(root))
	if err != nil || !res.Trusted {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	got, _ := certificates(res.Fullchain)
	if len(got) != 2 || got[0].Subject.CommonName != "example.com" || got[1].Subject.CommonName != "Test Intermediate" {
		t.Errorf("the chain must be the leaf then the intermediate, without the root: %d certificates", len(got))
	}
	if !strings.Contains(string(res.Key), "BEGIN PRIVATE KEY") || res.Issuer != "Test Intermediate" || res.NotAfter.Before(now) {
		t.Errorf("%+v", res)
	}

	// the whole bundle in the certificate box, and no chain box
	bundle := append(append([]byte{}, mid.pem...), leaf.pem...)
	if res, err = Check(key, bundle, nil, []string{"example.com"}, now, pool(root)); err != nil || !res.Trusted {
		t.Errorf("a bundle in one box: %v", err)
	}
}

func TestAMissingChainIsReportedButTheCertificateIsReturned(t *testing.T) {
	root, _ := issue(t, "Test Root", true, nil, nil, now.Add(-time.Hour*24), now.Add(time.Hour*24*3650))
	mid, _ := issue(t, "Test Intermediate", true, nil, &root, now.Add(-time.Hour*24), now.Add(time.Hour*24*3000))
	leaf, key := issue(t, "example.com", false, []string{"example.com"}, &mid, now.Add(-time.Hour), now.Add(time.Hour*24*90))
	res, err := Check(key, leaf.pem, nil, []string{"example.com"}, now, pool(root))
	if !errors.Is(err, ErrUntrusted) || res == nil || res.Trusted || len(res.Fullchain) == 0 {
		t.Fatalf("err=%v res=%v", err, res)
	}
	// a certificate from an authority nobody trusts, as an origin certificate behind a proxy is
	self, skey := issue(t, "example.com", false, []string{"example.com"}, nil, now.Add(-time.Hour), now.Add(time.Hour*24))
	if _, err := Check(skey, self.pem, nil, []string{"example.com"}, now, pool(root)); !errors.Is(err, ErrUntrusted) {
		t.Errorf("self-signed: %v", err)
	}
}

func TestWrongKeyExpiredAndUncoveredNamesAreRefused(t *testing.T) {
	root, _ := issue(t, "Test Root", true, nil, nil, now.Add(-time.Hour*24), now.Add(time.Hour*24*3650))
	leaf, key := issue(t, "example.com", false, []string{"example.com", "*.shop.example.com"}, &root, now.Add(-time.Hour), now.Add(time.Hour*24*90))
	_, otherKey := issue(t, "x", false, []string{"x.com"}, &root, now.Add(-time.Hour), now.Add(time.Hour))
	expired, ekey := issue(t, "example.com", false, []string{"example.com"}, &root, now.Add(-time.Hour*48), now.Add(-time.Hour*24))
	future, fkey := issue(t, "example.com", false, []string{"example.com"}, &root, now.Add(time.Hour*24), now.Add(time.Hour*48))
	for name, c := range map[string]struct {
		key, cert []byte
		names     []string
		want      string
	}{
		"another key":        {otherKey, leaf.pem, []string{"example.com"}, "does not belong"},
		"expired":            {ekey, expired.pem, []string{"example.com"}, "expired"},
		"not yet valid":      {fkey, future.pem, []string{"example.com"}, "valid only from"},
		"a name not covered": {key, leaf.pem, []string{"example.com", "other.com"}, "does not cover other.com"},
		"no certificate":     {key, []byte("nothing"), []string{"example.com"}, "no certificate"},
		"no key":             {[]byte("nothing"), leaf.pem, []string{"example.com"}, "no private key"},
	} {
		if _, err := Check(c.key, c.cert, nil, c.names, now, pool(root)); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v (want %q)", name, err, c.want)
		}
	}
	// a wildcard covers one label, not the bare name
	if _, err := Check(key, leaf.pem, nil, []string{"a.shop.example.com"}, now, pool(root)); err != nil {
		t.Errorf("wildcard: %v", err)
	}
	if _, err := Check(key, leaf.pem, nil, []string{"shop.example.com"}, now, pool(root)); err == nil {
		t.Error("*.shop.example.com must not cover shop.example.com")
	}
}

func TestKeysThatNginxCannotUseAreRefused(t *testing.T) {
	pass := pem.EncodeToMemory(&pem.Block{Type: "ENCRYPTED PRIVATE KEY", Bytes: []byte("x")})
	if _, err := ParseKey(pass); !errors.Is(err, ErrEncryptedKey) {
		t.Errorf("a passphrase: %v", err)
	}
	old := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Headers: map[string]string{"Proc-Type": "4,ENCRYPTED"}, Bytes: []byte("x")})
	if _, err := ParseKey(old); !errors.Is(err, ErrEncryptedKey) {
		t.Errorf("an old encrypted key: %v", err)
	}
	small, _ := rsa.GenerateKey(rand.Reader, 1024)
	der := x509.MarshalPKCS1PrivateKey(small)
	if _, err := ParseKey(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: der})); err == nil || !strings.Contains(err.Error(), "2048") {
		t.Errorf("a 1024-bit key: %v", err)
	}
	ec, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	ed, _ := x509.MarshalECPrivateKey(ec)
	if _, err := ParseKey(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: ed})); err != nil {
		t.Errorf("an EC key in the old format: %v", err)
	}
}

func TestACsrCarriesTheNamesAndItsKeyStaysWithUs(t *testing.T) {
	for _, algo := range []string{"", "ecdsa", "rsa"} {
		key, csrPEM, err := NewCSR(algo, []string{"example.com", "www.example.com"})
		if err != nil {
			t.Fatalf("%q: %v", algo, err)
		}
		block, _ := pem.Decode(csrPEM)
		csr, err := x509.ParseCertificateRequest(block.Bytes)
		if err != nil || csr.CheckSignature() != nil || csr.Subject.CommonName != "example.com" || len(csr.DNSNames) != 2 {
			t.Errorf("%q: %v %+v", algo, err, csr)
		}
		k, err := ParseKey(key)
		if err != nil || !samePublicKey(k.Public(), csr.PublicKey) {
			t.Errorf("%q: the key must be the one in the request: %v", algo, err)
		}
	}
	if _, _, err := NewCSR("dsa", []string{"a.com"}); err == nil {
		t.Error("an unknown key type must be refused")
	}
	if _, _, err := NewCSR("", nil); err == nil {
		t.Error("a request needs a name")
	}
}

func TestDescribeAndCovers(t *testing.T) {
	root, _ := issue(t, "Test Root", true, nil, nil, now.Add(-time.Hour*24), now.Add(time.Hour*24*3650))
	leaf, _ := issue(t, "example.com", false, []string{"example.com", "www.example.com"}, &root, now.Add(-time.Hour), now.Add(time.Hour*24*90))
	info, err := Describe(leaf.pem)
	if err != nil || info.Issuer != "Test Root" || len(info.Names) != 2 || !info.NotAfter.After(now) {
		t.Errorf("%+v %v", info, err)
	}
	if !Covers(leaf.pem, []string{"example.com", "www.example.com"}) || Covers(leaf.pem, []string{"other.com"}) || Covers([]byte("x"), []string{"a.com"}) {
		t.Error("Covers")
	}
}
