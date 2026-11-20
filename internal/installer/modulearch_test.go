package installer

import (
	"strings"
	"testing"
)

func TestModuleFileNamesTheCPU(t *testing.T) {
	if got := ModuleFile(ModPageSpeed, "trixie", "amd64"); got != "ngx_pagespeed_module-trixie-nginx1.30.7.so" {
		t.Errorf("amd64 keeps the old name, got %s", got)
	}
	if got := ModuleFile(ModModSecurity, "noble", "arm64"); got != "ngx_http_modsecurity_module-noble-nginx1.30.7-arm64.so" {
		t.Errorf("arm64 adds -arm64, got %s", got)
	}
}

// Every system that takes nginx from nginx.org has a pinned module for both CPUs.
func TestModuleSumsCoverBothCPUs(t *testing.T) {
	for kind, sums := range moduleSums {
		for _, c := range []string{"jammy", "noble", "resolute", "bookworm", "trixie"} {
			for _, k := range []string{moduleKey(c, "amd64"), moduleKey(c, "arm64")} {
				s := sums[k]
				if len(s) != 64 || strings.Trim(s, "0123456789abcdef") != "" {
					t.Errorf("%s %s: %q is not a SHA-256", kind, k, s)
				}
			}
		}
	}
}
