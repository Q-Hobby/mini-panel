package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"mpn/internal/backup"
	"mpn/internal/cloudsync"
	"mpn/internal/site"
	"mpn/internal/store"
)

const cloudFetchUnit = "mpn-cloud-fetch"

func readFetch(st *store.Store, siteName string) *cloudsync.FetchState {
	v, _ := st.Setting(cloudsync.FetchKey(siteName), "")
	if v == "" {
		return nil
	}
	var s cloudsync.FetchState
	if json.Unmarshal([]byte(v), &s) != nil {
		return nil
	}
	return &s
}

func writeFetch(st *store.Store, siteName string, s cloudsync.FetchState) {
	b, _ := json.Marshal(s)
	_ = st.SetSetting(cloudsync.FetchKey(siteName), string(b))
}

// effectiveFetch is the state of the last download as the panel should show it.
func effectiveFetch(st *store.Store, siteName string) *cloudsync.FetchState {
	s := readFetch(st, siteName)
	if s == nil {
		return nil
	}
	e := s.Effective(time.Now(), cloudsync.LockHeld(cloudsync.LockFile, processAlive))
	return &e
}

// cloudListCmd prints the finished copies of a site in the storage, and which of them are on this server already.
func cloudListCmd(name string) {
	if !site.ValidName(name) {
		fail(fmt.Errorf("invalid site name %q", name))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	r, ctx := openRemote(ctx)
	list, err := cloudsync.ListRemoteBackups(ctx, r, name)
	fail(err)
	local, _ := listStamps(name)
	have := map[string]bool{}
	for _, s := range local {
		have[s] = true
	}
	type row struct {
		Stamp  string  `json:"stamp"`
		SizeMB float64 `json:"size_mb"`
		Local  bool    `json:"local"`
	}
	rows := []row{}
	for _, x := range list {
		rows = append(rows, row{Stamp: x.Stamp, SizeMB: float64(x.Bytes) / (1 << 20), Local: have[x.Stamp]})
	}
	printJSON(rows)
}

// cloudFetchCmd brings one backup back from the storage into /var/backups/mpn/<site>/, where the usual restore finds it.
func cloudFetchCmd(st *store.Store, name, stamp string, detach, apply bool) {
	s, err := st.GetSite(name)
	fail(err)
	if !backup.ValidStamp(stamp) {
		fail(fmt.Errorf("invalid backup name %q", stamp))
	}
	if !apply {
		fmt.Printf("would download the backup %s of %s to %s\n", stamp, s.Name, backup.SiteDir(s.Name))
		return
	}
	requireLinuxRoot()
	if cloudConnected() == "" {
		fail(errors.New("cloud-not-connected: connect a storage first"))
	}
	if _, err := os.Stat(backup.Dir(s.Name, stamp)); err == nil {
		fail(fmt.Errorf("cloud-exists: the backup %s is on this server already", stamp))
	}
	if detach {
		prev := readFetch(st, s.Name)
		writeFetch(st, s.Name, cloudsync.FetchState{Stamp: stamp, At: time.Now().Unix(), Running: true})
		args := append([]string{"--collect", "--quiet", "--unit=" + cloudFetchUnit, "--description=mpn: download a backup"}, cloudLimits...)
		args = append(args, mpnPath, "cloud", "fetch", "-site", s.Name, "-backup", stamp, "-apply")
		out, err := exec.Command("systemd-run", args...).CombinedOutput()
		if err != nil {
			busy := strings.Contains(string(out), "already") || strings.Contains(string(out), "exists")
			if busy && prev != nil {
				writeFetch(st, s.Name, *prev)
			} else {
				writeFetch(st, s.Name, cloudsync.FetchState{Stamp: stamp, At: time.Now().Unix(), Message: "could not start: " + strings.TrimSpace(string(out))})
			}
			if busy {
				fail(errors.New("cloud-busy: a download is already running; wait for it to finish"))
			}
			fail(fmt.Errorf("could not start the download: %v %s", err, strings.TrimSpace(string(out))))
		}
		fmt.Println("the download has started in the background")
		return
	}
	lock, err := cloudsync.TakeLock(cloudsync.LockFile, processAlive)
	if errors.Is(err, cloudsync.ErrBusy) {
		writeFetch(st, s.Name, cloudsync.FetchState{Stamp: stamp, At: time.Now().Unix(), Message: "cloud-busy: a copy is already running"})
		fail(errors.New("cloud-busy: a copy is already running; wait for it to finish"))
	}
	fail(err)
	defer lock.Release()
	writeFetch(st, s.Name, cloudsync.FetchState{Stamp: stamp, At: time.Now().Unix(), Running: true})
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Hour)
	defer cancel()
	r, ctx := openRemote(ctx)
	// A database of another site, by the records, is refused; one this server has no record of (a site moved here) is let through,
	// and restoring it needs a record of this site (backupRestore).
	owner := func(db string) bool {
		rec, err := st.GetDatabase(db)
		return err != nil || rec.Site == s.Name
	}
	key, err := backup.LoadKey(backup.KeyFile)
	fail(err)
	err = cloudsync.FetchBackup(ctx, r, s.Name, stamp, backup.SiteDir(s.Name), freeBytes(backup.Base), os.Stdout, owner, key)
	state := cloudsync.FetchState{Stamp: stamp, At: time.Now().Unix(), OK: err == nil}
	if err != nil {
		state.Message = err.Error()
	}
	writeFetch(st, s.Name, state)
	fail(err)
	fmt.Printf("the backup %s of %s is on this server; restore it from the Backups tab\n", stamp, s.Name)
}
