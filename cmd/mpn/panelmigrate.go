package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"mpn/internal/distro"
	"mpn/internal/installer"
	"mpn/internal/osinfo"
	"mpn/internal/store"
)

// panelMigrate brings a machine installed by an earlier version up to date with what a first install of this one writes. install.sh
// runs it after it has replaced the program. It does nothing on a machine that is up to date, and changes nothing without -apply.
func panelMigrate(st *store.Store, apply bool) {
	if apply {
		// The copy to off-site storage needs mpn-cloud beside mpn (from 1.2.0). Failing to fetch it stops only that copy, so it
		// is said and the rest goes on.
		if err := ensureMpnCloud(); err != nil {
			fmt.Fprintf(os.Stderr, "could not install %s: %v; the copy to off-site storage will not work until mpn update -apply runs\n", mpnCloudFile, err)
		}
	}
	nginxUpgrade(apply)
	unit, err := os.ReadFile(installer.WebUnitFile)
	if os.IsNotExist(err) {
		fmt.Println("the web panel is not installed on this machine; nothing to do")
		return
	}
	fail(err)
	sock, _ := os.ReadFile(installer.SocketFile)
	if migrated, ok := installer.MigrateWebUnit(string(unit)); ok && migrated == string(unit) && string(sock) == installer.SocketUnit() && panelConfOnSocket() && installer.SELinuxUpToDate() {
		fmt.Println("the panel already listens on its socket; nothing to do")
		return
	}
	acts, err := installer.MigrateActions(installer.MigrateInput{
		WebUnit: string(unit), PanelPort: panelPortOf(), PMA: fileExists(installer.PMAConfFile), EL: distro.Current() == distro.EL,
		Ports: installer.PanelPorts(panelPortOf()),
	})
	fail(err)
	plan := installer.Plan{{Name: "the panel on its socket", Actions: acts}}
	if !apply {
		fmt.Print(plan)
		fmt.Println("# checked only. Add -apply to change this machine.")
		return
	}
	requireLinuxRoot()
	if err := runPlanQuiet(plan); err != nil {
		fail(fmt.Errorf("moving the panel to its socket failed: %w\nthe panel's own files are in /etc/systemd/system and /etc/nginx/conf.d; run mpn panel migrate -apply again once the cause is fixed", err))
	}
	// The blocks of the panel's name on port 443 and the pages of the database browser follow the same way.
	fail(syncFront(st))
	fmt.Println("the panel now listens on its socket, " + installer.PanelSocket)
}

// nginxUpgrade moves nginx from nginx.org to the version this release pins (installer.NginxVersion), with the modules the machine
// loads now built for that version. A machine already there, or one whose nginx comes from its distribution, is left alone.
func nginxUpgrade(apply bool) {
	raw, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return
	}
	t, err := osinfo.Parse(string(raw))
	if err != nil {
		return
	}
	v, _ := exec.Command("nginx", "-v").CombinedOutput() // "nginx -v" prints on standard error
	running := installer.NginxVersionOf(string(v))
	if !installer.NginxUpgradeNeeded(t, running) {
		return
	}
	var mods []string
	for _, kind := range []string{installer.ModPageSpeed, installer.ModModSecurity} {
		if installer.ModuleLoaded(kind) {
			mods = append(mods, kind)
		}
	}
	acts, err := installer.NginxUpgradeActions(installer.NginxUpgradeInput{Target: t, Running: running, Modules: mods})
	fail(err)
	plan := installer.Plan{{Name: "nginx " + running + " to " + installer.NginxVersion, Actions: acts}}
	if !apply {
		fmt.Print(plan)
		fmt.Println("# checked only. Add -apply to change this machine.")
		return
	}
	requireLinuxRoot()
	if err := runPlanQuiet(plan); err != nil {
		fail(fmt.Errorf("moving nginx to %s failed: %w\nnginx %s and its modules were put back; the files before the change are in /var/backups/mpn-nginx-upgrade/old", installer.NginxVersion, err, running))
	}
}

// panelConfOnSocket says whether the panel's nginx file points at the socket.
func panelConfOnSocket() bool {
	b, err := os.ReadFile(installer.PanelConfFile)
	return err == nil && strings.Contains(string(b), "proxy_pass "+installer.PanelUpstream+";")
}
