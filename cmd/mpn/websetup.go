package main

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"mpn/internal/webauth"
)

// setupKeyPath is the file that holds the key of the setup link, next to the account file in the panel's own folder. The panel reads
// it and removes it when the first account is made.
func setupKeyPath(account string) string { return filepath.Join(filepath.Dir(account), "setup-key") }

var setupKeyRe = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

// webSetupLink prints the link that opens the setup page, making its key first when there is none. It runs as root; the key file is
// given to the panel's account, which is the only other one that can read it. Once an account exists there is nothing to set up.
func webSetupLink(args []string) {
	fs := flag.NewFlagSet("web setup-link", flag.ExitOnError)
	account := fs.String("account", defaultAccount, "file with the administrator account")
	webUser := fs.String("web-user", "mpn-web", "account the panel runs as")
	quiet := fs.Bool("quiet", false, "print only the address, nothing when the panel is set up already")
	_ = fs.Parse(args)
	if runtime.GOOS != "linux" || os.Geteuid() != 0 {
		fail(errors.New("the setup link needs Linux and root"))
	}
	if _, err := os.Stat(*account); err == nil || !errors.Is(err, os.ErrNotExist) {
		if !*quiet {
			fmt.Println("the panel has an account already; sign in, or set a new password with: mpn web passwd")
		}
		return
	}
	key, err := ensureSetupKey(setupKeyPath(*account), *webUser)
	fail(err)
	host, port := serverAddress(), panelPortOf()
	if port == 0 {
		port = 2610
	}
	link := fmt.Sprintf("https://%s:%d/setup?t=%s", host, port, key)
	if *quiet {
		fmt.Println(link)
		return
	}
	fmt.Printf("Open this link to create the panel's account (it works until the account exists):\n\n  %s\n\n", link)
}

// ensureSetupKey returns the key in path, or writes a new random one (256 bits) owned by the panel's account.
func ensureSetupKey(path, webUser string) (string, error) {
	if b, err := os.ReadFile(path); err == nil {
		if k := strings.TrimSpace(string(b)); setupKeyRe.MatchString(k) {
			return k, nil
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	u, err := user.Lookup(webUser)
	if err != nil {
		return "", err
	}
	uid, err1 := strconv.Atoi(u.Uid)
	gid, err2 := strconv.Atoi(u.Gid)
	if err1 != nil || err2 != nil || uid == 0 {
		return "", errors.New("the web account has an id mpn cannot use")
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	key := base64.RawURLEncoding.EncodeToString(b)
	// Written like the account: a temporary file in the folder, owned by the panel's account, then moved into place.
	if err := webauth.WriteOwned(path, []byte(key+"\n"), uid, gid); err != nil {
		return "", err
	}
	return key, nil
}

// serverAddress is the address a person types to reach this machine: its public IPv4 address when it has one, else its name.
func serverAddress() string {
	if ip := strings.TrimSpace(out(3*time.Second, "sh", "-c", `ip -4 route get 1.1.1.1 2>/dev/null | awk '{ for (i = 1; i < NF; i++) if ($i == "src") { print $(i + 1); exit } }'`)); ip != "" {
		return ip
	}
	h, _ := os.Hostname()
	if h == "" {
		h = "<server-ip>"
	}
	return h
}
