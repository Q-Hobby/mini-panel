// Package cloudsync copies the backups of a site to storage outside the server and keeps the copies tidy. It knows nothing about the
// storage: it works with the small Remote interface, so it can be tested with storage in memory.
//
// In the storage a backup is a folder <site>/<stamp>/ under mpn-backups, holding the same files as /var/backups/mpn/<site>/<stamp>/.
// MANIFEST is uploaded last, so a folder without it is a copy that was cut off, and is made again.
package cloudsync

import (
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"

	"mpn/internal/backup"
	"mpn/internal/site"
)

// Entry is one item of a listing of the storage.
type Entry struct {
	Name string
	Dir  bool
	Size int64
}

// Remote is what the sync needs from the storage. Paths are relative to the folder mpn-backups and use "/"; "" is that folder.
type Remote interface {
	List(ctx context.Context, dir string) ([]Entry, error) // a folder that does not exist is empty
	Upload(ctx context.Context, dir, name, localPath string) error
	Download(ctx context.Context, remotePath, localPath string) error // checks what arrived as well as the storage allows
	Purge(ctx context.Context, dir string) error                      // a folder that is already gone is not an error
	Mkdir(ctx context.Context, dir string) error
}

// fileRe names the files of a backup that are uploaded; anything else in the folder stays where it is.
var fileRe = regexp.MustCompile(`^(files\.tar\.gz|[A-Za-z0-9_]{1,64}\.sql\.gz|MANIFEST)$`)

// Result says what a sync did.
type Result struct {
	Uploaded []string // stamps copied now
	Deleted  []string // stamps removed from the storage
	Present  int      // finished copies in the storage afterwards
}

// remote holds the finished and the unfinished copies of a site, by stamp.
type remote struct{ complete, partial map[string]bool }

func listRemote(ctx context.Context, r Remote, siteName string) (remote, error) {
	out := remote{complete: map[string]bool{}, partial: map[string]bool{}}
	folders, err := r.List(ctx, siteName)
	if err != nil {
		return out, err
	}
	for _, f := range folders {
		if !f.Dir || !backup.ValidStamp(f.Name) {
			continue
		}
		inside, err := r.List(ctx, siteName+"/"+f.Name)
		if err != nil {
			return out, err
		}
		done := false
		for _, x := range inside {
			if x.Name == "MANIFEST" && !x.Dir {
				done = true
			}
		}
		if done {
			out.complete[f.Name] = true
		} else {
			out.partial[f.Name] = true
		}
	}
	return out, nil
}

// filesOf returns the files of a backup in the order to upload them: MANIFEST last.
func filesOf(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	hasManifest := false
	for _, e := range entries {
		if !e.Type().IsRegular() || !fileRe.MatchString(e.Name()) {
			continue
		}
		if e.Name() == "MANIFEST" {
			hasManifest = true
			continue
		}
		names = append(names, e.Name())
	}
	if !hasManifest {
		return nil, fmt.Errorf("%s has no MANIFEST: the backup is not finished", dir)
	}
	sort.Strings(names)
	return append(names, "MANIFEST"), nil
}

// SyncSite makes the storage hold the newest `keep` finished backups of the site. local is the list of finished local stamps and dirOf
// the folder of one of them. The newest are uploaded first, so the most useful copy is there before the others. A failure stops at the
// first problem: what was uploaded stays, and the next sync goes on from there.
func SyncSite(ctx context.Context, r Remote, siteName string, local []string, dirOf func(stamp string) string, keep int, log io.Writer) (Result, error) {
	var res Result
	if !site.ValidName(siteName) {
		return res, fmt.Errorf("invalid site name %q", siteName)
	}
	if keep < 1 {
		return res, fmt.Errorf("keep at least one backup")
	}
	if err := r.Mkdir(ctx, siteName); err != nil {
		return res, err
	}
	rem, err := listRemote(ctx, r, siteName)
	if err != nil {
		return res, err
	}
	// The newest `keep` local backups are the ones worth having in the storage.
	want := append([]string(nil), local...)
	sort.Sort(sort.Reverse(sort.StringSlice(want)))
	if len(want) > keep {
		want = want[:keep]
	}
	for _, stamp := range want {
		if !backup.ValidStamp(stamp) || rem.complete[stamp] {
			continue
		}
		dir := siteName + "/" + stamp
		if rem.partial[stamp] {
			fmt.Fprintf(log, "%s: the copy of %s in the storage was cut off, making it again\n", siteName, stamp)
			if err := r.Purge(ctx, dir); err != nil {
				return res, err
			}
			delete(rem.partial, stamp)
		}
		names, err := filesOf(dirOf(stamp))
		if err != nil {
			return res, err
		}
		if err := r.Mkdir(ctx, dir); err != nil {
			return res, err
		}
		for _, n := range names {
			fmt.Fprintf(log, "%s: uploading %s\n", dir, n)
			if err := r.Upload(ctx, dir, n, dirOf(stamp)+"/"+n); err != nil {
				return res, fmt.Errorf("%s/%s: %w", dir, n, err)
			}
		}
		rem.complete[stamp] = true
		res.Uploaded = append(res.Uploaded, stamp)
	}
	// Tidy up: only folders named like a backup are ever touched.
	var done []string
	for s := range rem.complete {
		done = append(done, s)
	}
	for _, stamp := range backup.Expired(done, keep) {
		fmt.Fprintf(log, "%s: removing the old copy %s from the storage\n", siteName, stamp)
		if err := r.Purge(ctx, siteName+"/"+stamp); err != nil {
			return res, err
		}
		delete(rem.complete, stamp)
		res.Deleted = append(res.Deleted, stamp)
	}
	newest := ""
	for s := range rem.complete {
		if s > newest {
			newest = s
		}
	}
	for stamp := range rem.partial { // a cut-off copy older than the newest finished one will never be finished
		if stamp < newest {
			if err := r.Purge(ctx, siteName+"/"+stamp); err != nil {
				return res, err
			}
			res.Deleted = append(res.Deleted, stamp)
		}
	}
	sort.Strings(res.Uploaded)
	sort.Strings(res.Deleted)
	res.Present = len(rem.complete)
	return res, nil
}
