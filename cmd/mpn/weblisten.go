package main

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

// webListener returns where the panel takes its connections.
//
//   - "systemd": the socket systemd made and passed on (mpn-web.socket, fd 3). It is the way the panel runs on a server: the socket
//     lives in a folder only root can write to, only nginx and the database browser may open it, and it stays while the panel
//     restarts, so no other program can take its place.
//   - "unix:PATH": a socket the panel makes itself, for trying the panel by hand.
//   - "127.0.0.1:PORT": a loopback port, for development only. Any program of the machine could take such a port while the panel
//     is down, so the client address a request carries is never believed there (see trusted below).
func webListener(spec string) (l net.Listener, trusted bool, err error) {
	switch {
	case spec == "systemd":
		if os.Getenv("LISTEN_PID") != strconv.Itoa(os.Getpid()) || os.Getenv("LISTEN_FDS") != "1" {
			return nil, false, errors.New("-listen systemd: no socket was passed (start the panel through mpn-web.socket)")
		}
		os.Unsetenv("LISTEN_PID")
		os.Unsetenv("LISTEN_FDS")
		os.Unsetenv("LISTEN_FDNAMES")
		f := os.NewFile(3, "mpn-web.socket")
		l, err = net.FileListener(f)
		f.Close()
		if err != nil {
			return nil, false, err
		}
		if l.Addr().Network() != "unix" {
			l.Close()
			return nil, false, fmt.Errorf("-listen systemd: the socket passed is %s, not a Unix socket", l.Addr().Network())
		}
		return l, true, nil
	case strings.HasPrefix(spec, "unix:"):
		path := strings.TrimPrefix(spec, "unix:")
		if !strings.HasPrefix(path, "/") {
			return nil, false, errors.New("-listen unix:PATH needs an absolute path")
		}
		l, err = net.Listen("unix", path)
		return l, true, err
	}
	host, _, err := net.SplitHostPort(spec)
	if err != nil {
		return nil, false, err
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return nil, false, fmt.Errorf("-listen %s: the panel only listens on a loopback address; reach it through nginx with TLS", spec)
	}
	l, err = net.Listen("tcp", spec)
	return l, false, err
}
