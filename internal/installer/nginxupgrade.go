package installer

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"mpn/internal/distro"
	"mpn/internal/osinfo"
)

// Moving a machine that runs an older nginx from nginx.org (1.28.3 before 1.2.2) to NginxVersion. install.sh and "mpn update" replace
// only the program, so "mpn panel migrate" runs this too. A dynamic module only loads into the exact nginx it was built for, so the
// modules the machine loads are swapped for the ones built for the new version in the same step as the package, and a failed
// "nginx -t" puts the old package, the old modules and the old pin back.

// NginxUpgradeNeeded says whether the machine should move to NginxVersion from nginx.org: it is a system that takes nginx from
// there, and it runs another version or the nginx of its distribution. A machine on Ubuntu 26.04 installed before 1.2.3 runs the
// nginx of Ubuntu, and is moved too.
func NginxUpgradeNeeded(t osinfo.Target, running string, distroBuild bool) bool {
	if !t.NginxOrg() || running == "" {
		return false
	}
	if distroBuild {
		// Only Ubuntu 26.04 had the distribution's nginx by design. A machine of another system with it was installed before the
		// panel pinned one version, and keeps it, as WAFPackagesPlan does.
		return t.ID == "ubuntu" && t.Version == "26.04"
	}
	return running != NginxVersion
}

// DistroNginx says whether the output of "nginx -v" is that of the distribution's nginx: Ubuntu and Debian name themselves after the
// version ("nginx/1.28.3 (Ubuntu)"), nginx.org's package prints the bare version.
func DistroNginx(nginxV string) bool {
	return strings.Contains(nginxV, "(Ubuntu)") || strings.Contains(nginxV, "(Debian)")
}

// distroNginxPackages are the packages of the distribution's nginx and of the modules built for it that a machine on Ubuntu 26.04
// of 1.2.2 or older has. nginx.org's package conflicts with nginx-common, and the modules depend on the ABI of that nginx, so all of
// them leave in the same apt run that brings nginx.org's in. They are removed, not purged: their files in /etc/nginx stay.
var distroNginxPackages = []string{"nginx", "nginx-common", "nginx-core", "nginx-full", "nginx-light", "nginx-extras",
	"nginx-module-pagespeed", "libnginx-mod-http-modsecurity", "libnginx-mod-http-ndk"}

// nginxAptFiles are the apt files the move changes, kept and put back as they were (or removed when they were not there).
var nginxAptFiles = []string{"/etc/apt/sources.list.d/nginx.list", "/etc/apt/sources.list.d/modpagespeed.list", "/etc/apt/preferences.d/99-mpn-nginx"}

// NginxUpgradeInput is what the machine has now.
type NginxUpgradeInput struct {
	Target  osinfo.Target
	Running string   // the version "nginx -v" prints, such as 1.28.3
	Distro  bool     // the running nginx is the distribution's (see DistroNginx), not nginx.org's
	Modules []string // the kinds of module this machine loads now (ModPageSpeed, ModModSecurity)
}

// nginxUpgradeBackup is where the old modules and the old pin are kept until the new nginx passes "nginx -t".
const nginxUpgradeBackup = "/var/backups/mpn-nginx-upgrade"

// NginxUpgradeActions are the steps that move nginx to NginxVersion with the modules the machine loads. Nothing runs when the machine
// does not need it.
func NginxUpgradeActions(in NginxUpgradeInput) ([]Action, error) {
	t := in.Target
	if !NginxUpgradeNeeded(t, in.Running, in.Distro) {
		return nil, nil
	}
	if !validVersion(in.Running) {
		return nil, fmt.Errorf("the running nginx version %q could not be read", in.Running)
	}
	dir := distro.NginxModuleDir()
	var fetch []string // one download per module, into the backup folder first, so nothing changes before every file is here
	var put []string
	for _, kind := range in.Modules {
		n, ok := moduleNames[kind]
		if !ok {
			return nil, fmt.Errorf("unknown module kind %q", kind)
		}
		url, sum, err := moduleSource(t, kind)
		if err != nil {
			return nil, err
		}
		staged := nginxUpgradeBackup + "/new/" + n.so
		fetch = append(fetch, fetchScript(url, sum, staged))
		put = append(put, "install -m 0644 -o root -g root "+staged+" "+dir+"/"+n.so)
		if in.Distro {
			// The distribution's snippet is a link into its package, gone once the package is; the panel's own snippet replaces it.
			snip := nginxModulesEnabled + "/" + n.snippet
			put = append(put, "rm -f "+snip, "printf '# Written by mpn.\\nload_module %s;\\n' "+dir+"/"+n.so+" > "+snip)
			if kind == ModModSecurity {
				// The module of nginx.org links libmodsecurity, which apt installed only for the package that is now gone; it must not be
				// taken by an autoremove. modsecurity.conf is the one a WAF on nginx.org's nginx gets (WAFPackagesPlan).
				// modsecurity.conf of the removed package stays (removed, not purged) and has the settings the rules need, as on bay.onl.
				put = append(put, "apt-mark manual libmodsecurity3t64 >/dev/null 2>&1 || apt-mark manual libmodsecurity3 >/dev/null")
			}
		}
	}

	acts := []Action{Run("sh", "-c", "rm -rf "+nginxUpgradeBackup+"/new && install -d -m 0700 "+nginxUpgradeBackup+"/new")}
	for _, f := range fetch {
		acts = append(acts, Run("sh", "-c", f))
	}
	acts = append(acts, Run("sh", "-c", nginxUpgradeScript(t, in.Running, in.Distro, dir, put)))
	acts = append(acts, ReloadNginx())
	return acts, nil
}

// ModuleLoaded says whether nginx is told to load the module of one kind (its snippet in modules-enabled).
func ModuleLoaded(kind string) bool {
	n, ok := moduleNames[kind]
	if !ok {
		return false
	}
	_, err := os.Stat(nginxModulesEnabled + "/" + n.snippet)
	return err == nil
}

// moduleSource is the address and checksum of the module of one kind built for NginxVersion on this system and CPU.
func moduleSource(t osinfo.Target, kind string) (url, sum string, err error) {
	if t.EL() && kind == ModModSecurity {
		name := "ngx_http_modsecurity_module-" + t.Codename + "-nginx" + NginxVersion + archSuffix() + ".so"
		sum = elSums[name]
		if sum == "" {
			return "", "", fmt.Errorf("there is no checksum for %s in this program", name)
		}
		return elPackagesBase + "/" + name, sum, nil
	}
	sum, ok := moduleSums[kind][moduleKey(t.Codename, runtime.GOARCH)]
	if !ok {
		return "", "", fmt.Errorf("there is no %s module for nginx %s on %s %s (%s)", kind, NginxVersion, t.ID, t.Version, runtime.GOARCH)
	}
	return moduleRelease(t) + "/" + ModuleFile(kind, t.Codename, runtime.GOARCH), sum, nil
}

func validVersion(v string) bool {
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return false
	}
	for _, p := range parts {
		if p == "" || strings.Trim(p, "0123456789") != "" {
			return false
		}
	}
	return true
}

// shQuote puts a script in single quotes for "sh -c".
func shQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// nginxUpgradeScript keeps the old modules and pin, installs the new package and modules, and checks the configuration. When the
// check fails, it installs the old packages again with the old modules and pin, so the machine is left as it was. distroBuild is set
// when the machine runs the distribution's nginx: then nginx.org's repository is added and the distribution's packages leave.
func nginxUpgradeScript(t osinfo.Target, old string, distroBuild bool, dir string, put []string) string {
	b := nginxUpgradeBackup
	// install is a list of commands, each run as "cmd || restore". They are not put in a block: a here-document inside an indented
	// block would not find its end line, and distro.RefreshScript ends with "exit", which would end this whole script.
	var install []string
	var pin, unpin, reinstallOld, keep, restoreFiles string
	if t.EL() {
		unpin = "dnf -q versionlock delete nginx >/dev/null 2>&1 || true"
		install = []string{"dnf -y -q install nginx-" + NginxVersion}
		pin = "dnf -q versionlock add nginx"
		reinstallOld = "dnf -y -q downgrade nginx-" + old + " || dnf -y -q install nginx-" + old
		keep, restoreFiles = "true", "true"
	} else {
		list, newPrefs := nginxOrgSources(t)
		apt := "DEBIAN_FRONTEND=noninteractive apt-get -o DPkg::Lock::Timeout=300 -o Dpkg::Options::=--force-confold install -y --allow-change-held-packages "
		refresh := "sh -c " + shQuote(distro.RefreshScript())
		unpin = "apt-mark unhold nginx >/dev/null"
		// The new pin goes in before the install: the old one names the old version with a priority above 1000, and apt would keep it.
		install = []string{"printf '%s' '" + newPrefs + "' > /etc/apt/preferences.d/99-mpn-nginx"}
		newPkg := "nginx=" + NginxVersion + "-1~" + t.Codename
		reinstallOld = apt + "--allow-downgrades nginx=" + old + "-1~" + t.Codename
		if distroBuild {
			install = append(install,
				"sh -c "+shQuote(nginxOrgKeyScript),
				"printf '%s' '"+list+"' > /etc/apt/sources.list.d/nginx.list",
				// We-Amp's repository has the PageSpeed of the distribution's nginx, which the module of mpn-packages replaces.
				"rm -f /etc/apt/sources.list.d/modpagespeed.list",
			)
			// "name-" in an apt install removes the package in the same run. nginx itself is not removed: it is the one installed.
			newPkg += " $(awk -F= '$1 != \"nginx\" {print $1\"-\"}' $b/old/packages)"
			reinstallOld = apt + "--allow-downgrades $(cat $b/old/packages)"
		}
		install = append(install, refresh, apt+newPkg)
		if distroBuild {
			// The distribution's links into its modules are left pointing at nothing, and its package's default site would answer on
			// port 80 next to the panel's; the panel writes its own snippets (see put).
			install = append(install, "find "+nginxModulesEnabled+" -xtype l -delete", "rm -f /etc/nginx/conf.d/default.conf")
		}
		pin = "apt-mark hold nginx >/dev/null"
		keep = `install -d -m 0700 $b/old/apt
dpkg-query -W -f='${Package}=${Version} ${db:Status-Status}\n' ` + strings.Join(distroNginxPackages, " ") + ` 2>/dev/null | awk '$2 == "installed" {print $1}' > $b/old/packages
for f in ` + strings.Join(nginxAptFiles, " ") + `; do [ ! -e "$f" ] || cp -p "$f" $b/old/apt/; done`
		restoreFiles = `for f in ` + strings.Join(nginxAptFiles, " ") + `; do if [ -e "$b/old/apt/${f##*/}" ]; then cp -p "$b/old/apt/${f##*/}" "$f"; else rm -f "$f"; fi; done
  ` + refresh + ` || true`
	}
	for i, c := range install {
		install[i] = c + " || restore"
	}
	return `set -eu
b=` + b + `; dir=` + dir + `
rm -rf $b/old; install -d -m 0700 $b/old
cp -a /etc/nginx $b/old/etc-nginx
for f in $dir/*.so; do [ -e "$f" ] && cp -p "$f" $b/old/; done
` + keep + `
restore() {
  echo "nginx ` + NginxVersion + ` did not pass its check; putting nginx ` + old + ` back" >&2
  ` + restoreFiles + `
  ` + reinstallOld + ` || true
  for f in $b/old/*.so; do [ -e "$f" ] && cp -p "$f" $dir/; done
  rm -rf /etc/nginx && cp -a $b/old/etc-nginx /etc/nginx
  ` + pinBack(t) + `
  nginx -t && systemctl restart nginx
  exit 1
}
` + unpin + `
` + strings.Join(install, "\n") + `
` + strings.Join(put, "\n") + `
v=$(nginx -v 2>&1)
case "$v" in *nginx/` + NginxVersion + `*) ;; *) restore ;; esac
nginx -t || restore
` + pin + `
systemctl restart nginx || restore
echo "nginx ` + old + ` -> ` + NginxVersion + `"
`
}

// pinBack holds the old package again after restore() put it back.
func pinBack(t osinfo.Target) string {
	if t.EL() {
		return "dnf -q versionlock add nginx || true"
	}
	return "apt-mark hold nginx >/dev/null || true"
}
