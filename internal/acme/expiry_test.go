package acme

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"
)

func certPEM(t *testing.T, notAfter time.Time) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "t"},
		NotBefore: notAfter.Add(-90 * 24 * time.Hour), NotAfter: notAfter}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func TestDaysLeft(t *testing.T) {
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name     string
		notAfter time.Time
		want     int
	}{
		{"ten and a half days", now.Add(10*24*time.Hour + 12*time.Hour), 10},
		{"under one day", now.Add(5 * time.Hour), 0},
		{"expired half a day ago", now.Add(-12 * time.Hour), -1},
	}
	for _, c := range cases {
		got, err := DaysLeft(certPEM(t, c.notAfter), now)
		if err != nil || got != c.want {
			t.Errorf("%s: got %d err=%v want %d", c.name, got, err, c.want)
		}
	}
}

func TestDaysLeftRejectsGarbage(t *testing.T) {
	for _, in := range [][]byte{nil, []byte("hello"), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: []byte{1}}),
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte{1, 2}})} {
		if _, err := DaysLeft(in, time.Now()); err == nil {
			t.Errorf("input %q must be rejected", in)
		}
	}
}
