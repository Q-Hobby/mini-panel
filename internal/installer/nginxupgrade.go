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

// NginxUpgradeNeeded says whether the machine should move to NginxVersion: it takes nginx from nginx.org (every supported system
// but Ubuntu 26.04, and the RHEL family) and runs another version.
func NginxUpgradeNeeded(t osinfo.Target, running string) bool {
	return (t.NginxOrg() || t.EL()) && running != "" && running != NginxVersion
}

// NginxUpgradeInput is what the machine has now.
type NginxUpgradeInput struct {
	Target  osinfo.Target
	Running string   // the version "nginx -v" prints, such as 1.28.3
	Modules []string // the kinds of module this machine loads now (ModPageSpeed, ModModSecurity)
}

// nginxUpgradeBackup is where the old modules and the old pin are kept until the new nginx passes "nginx -t".
const nginxUpgradeBackup = "/var/backups/mpn-nginx-upgrade"

// NginxUpgradeActions are the steps that move nginx to NginxVersion with the modules the machine loads. Nothing runs when the machine
// does not need it.
func NginxUpgradeActions(in NginxUpgradeInput) ([]Action, error) {
	t := in.Target
	if !NginxUpgradeNeeded(t, in.Running) {
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
	}

	acts := []Action{Run("sh", "-c", "rm -rf "+nginxUpgradeBackup+"/new && install -d -m 0700 "+nginxUpgradeBackup+"/new")}
	for _, f := range fetch {
		acts = append(acts, Run("sh", "-c", f))
	}
	acts = append(acts, Run("sh", "-c", nginxUpgradeScript(t, in.Running, dir, put)))
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

// nginxUpgradeScript keeps the old modules and pin, installs the new package and modules, and checks the configuration. When the
// check fails, it installs the old package again with the old modules and pin, so the machine is left as it was.
func nginxUpgradeScript(t osinfo.Target, old, dir string, put []string) string {
	b := nginxUpgradeBackup
	// install is a list of commands, each run as "cmd || restore". They are not put in a block: a here-document inside an indented
	// block would not find its end line, and distro.RefreshScript ends with "exit", which would end this whole script.
	var install []string
	var pin, unpin, reinstallOld, keepPin, restorePin string
	if t.EL() {
		unpin = "dnf -q versionlock delete nginx >/dev/null 2>&1 || true"
		install = []string{"dnf -y -q install nginx-" + NginxVersion}
		pin = "dnf -q versionlock add nginx"
		reinstallOld = "dnf -y -q downgrade nginx-" + old + " || dnf -y -q install nginx-" + old
		keepPin, restorePin = "true", "true"
	} else {
		prefs := "/etc/apt/preferences.d/99-mpn-nginx"
		_, newPrefs := nginxOrgSources(t)
		apt := "DEBIAN_FRONTEND=noninteractive apt-get -o DPkg::Lock::Timeout=300 -o Dpkg::Options::=--force-confold install -y --allow-change-held-packages "
		unpin = "apt-mark unhold nginx >/dev/null"
		// The new pin goes in before the install: the old one names the old version with a priority above 1000, and apt would keep it.
		install = []string{
			"printf '%s' '" + newPrefs + "' > " + prefs,
			"sh -c '" + strings.ReplaceAll(distro.RefreshScript(), "'", `'\''`) + "'",
			apt + "nginx=" + NginxVersion + "-1~" + t.Codename,
		}
		pin = "apt-mark hold nginx >/dev/null"
		reinstallOld = apt + "--allow-downgrades nginx=" + old + "-1~" + t.Codename
		keepPin = "[ ! -f " + prefs + " ] || cp -p " + prefs + " " + b + "/99-mpn-nginx"
		restorePin = "[ ! -f " + b + "/99-mpn-nginx ] || cp -p " + b + "/99-mpn-nginx " + prefs
	}
	for i, c := range install {
		install[i] = c + " || restore"
	}
	return `set -eu
b=` + b + `; dir=` + dir + `
rm -rf $b/old; install -d -m 0700 $b/old
cp -a /etc/nginx $b/old/etc-nginx
for f in $dir/*.so; do [ -e "$f" ] && cp -p "$f" $b/old/; done
` + keepPin + `
restore() {
  echo "nginx ` + NginxVersion + ` did not pass its check; putting nginx ` + old + ` back" >&2
  ` + restorePin + `
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
