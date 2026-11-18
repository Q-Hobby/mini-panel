package security

import (
	"fmt"
	"strings"
)

// The sites behind Cloudflare see every visitor as an address of Cloudflare: the logs, the limits of nginx, fail2ban and the web
// application firewall would all act on Cloudflare instead of on the visitor. With this on, nginx takes the visitor's address from
// CF-Connecting-IP, but only when the connection comes from one of Cloudflare's own networks; a header sent from anywhere else is
// ignored, so nobody can choose their address by sending it. It is off unless the owner turns it on, because it changes the
// address every site sees.

// CloudflareConf is the file that holds it; nginx reads every file of conf.d inside http { }.
const CloudflareConf = "/etc/nginx/conf.d/05-mpn-cloudflare.conf"

// CloudflareNets are the networks Cloudflare publishes at https://www.cloudflare.com/ips-v4 and /ips-v6 (taken on 2026-11-17).
var CloudflareNets = []string{
	"173.245.48.0/20", "103.21.244.0/22", "103.22.200.0/22", "103.31.4.0/22", "141.101.64.0/18", "108.162.192.0/18",
	"190.93.240.0/20", "188.114.96.0/20", "197.234.240.0/22", "198.41.128.0/17", "162.158.0.0/15", "104.16.0.0/13",
	"104.24.0.0/14", "172.64.0.0/13", "131.0.72.0/22",
	"2400:cb00::/32", "2606:4700::/32", "2803:f800::/32", "2405:b500::/32", "2405:8100::/32", "2a06:98c0::/29", "2c0f:f248::/32",
}

// CloudflareText is the content of CloudflareConf.
func CloudflareText() string {
	var b strings.Builder
	b.WriteString("# Written by mpn (mpn security cloudflare). The visitor's address comes from CF-Connecting-IP, only on connections from Cloudflare.\n")
	for _, n := range CloudflareNets {
		fmt.Fprintf(&b, "set_real_ip_from %s;\n", n)
	}
	b.WriteString("real_ip_header CF-Connecting-IP;\n")
	return b.String()
}
