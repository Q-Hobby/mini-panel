package cloudsync

import (
	"bufio"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"mpn/internal/backup"
	"mpn/internal/dbadmin"
	"mpn/internal/site"
)

// RemoteBackup is a finished copy in the storage.
type RemoteBackup struct {
	Stamp string `json:"stamp"`
	Bytes int64  `json:"bytes"`
}

// backupFiles keeps the files of a folder that belong to a backup. A name that is not one of those is never downloaded, so a stranger's
// file in the folder cannot end up in /var/backups.
func backupFiles(in []Entry) map[string]Entry {
	out := map[string]Entry{}
	for _, x := range in {
		if !x.Dir && fileRe.MatchString(x.Name) {
			if _, dup := out[x.Name]; !dup {
				out[x.Name] = x
			}
		}
	}
	return out
}

// ListRemoteBackups returns the finished copies of a site in the storage, newest first. A copy without MANIFEST was cut off and is left out.
func ListRemoteBackups(ctx context.Context, r Remote, siteName string) ([]RemoteBackup, error) {
	if !site.ValidName(siteName) {
		return nil, fmt.Errorf("invalid site name %q", siteName)
	}
	stamps, err := r.List(ctx, siteName)
	if err != nil {
		return nil, err
	}
	var out []RemoteBackup
	for _, s := range stamps {
		if !s.Dir || !backup.ValidStamp(s.Name) {
			continue
		}
		inside, err := r.List(ctx, siteName+"/"+s.Name)
		if err != nil {
			return nil, err
		}
		files := backupFiles(inside)
		if _, done := files["MANIFEST"]; !done {
			continue
		}
		var n int64
		for _, f := range files {
			n += f.Size
		}
		out = append(out, RemoteBackup{Stamp: s.Name, Bytes: n})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Stamp > out[j].Stamp })
	return out, nil
}

// gzipOK reads a .gz file to its end, the check "gzip -t" makes: a cut-off or damaged file fails. Like gzip -t it accepts several
// members one after the other, and zero bytes after the last one: GNU tar pads what it writes to a whole record (10240 bytes), and
// Go's reader alone would take that padding for a broken member.
func gzipOK(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	br := bufio.NewReader(f)
	zr, err := gzip.NewReader(br)
	if err != nil {
		return err
	}
	for {
		zr.Multistream(false)
		if _, err := io.Copy(io.Discard, zr); err != nil {
			return err
		}
		next, err := br.Peek(1)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if next[0] == 0 {
			return onlyZeros(br)
		}
		if err := zr.Reset(br); err != nil {
			return err
		}
	}
}

// onlyZeros reports an error unless everything left in r is zero bytes.
func onlyZeros(r io.Reader) error {
	buf := make([]byte, 32<<10)
	for {
		n, err := r.Read(buf)
		for _, b := range buf[:n] {
			if b != 0 {
				return errors.New("data after the end of the compressed stream")
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func download(ctx context.Context, r Remote, dir string, f Entry, to string, log io.Writer) error {
	p := filepath.Join(to, f.Name)
	fmt.Fprintf(log, "downloading %s (%.1f MB)\n", f.Name, float64(f.Size)/(1<<20))
	if err := r.Download(ctx, dir+"/"+f.Name, p); err != nil {
		return fmt.Errorf("%s arrived damaged or not at all: %w", f.Name, err)
	}
	st, err := os.Stat(p)
	if err != nil {
		return err
	}
	if st.Size() != f.Size {
		return fmt.Errorf("%s arrived damaged: %d bytes, the storage has %d", f.Name, st.Size(), f.Size)
	}
	if filepath.Ext(f.Name) == ".gz" {
		if err := gzipOK(p); err != nil {
			return fmt.Errorf("%s arrived damaged: it is not a whole gzip file: %w", f.Name, err)
		}
	}
	return nil
}

// FetchBackup copies one finished backup of a site from the storage into siteDir/<stamp>, where the usual restore finds it. It is
// downloaded into a folder of a different name and renamed at the end, so the backup appears whole or not at all. Everything that
// comes from the storage is checked before it counts: the MANIFEST is read first and must be for this site and stamp; rclone compares
// every file with the hash the storage holds when it has one, the size is compared always, and every .gz is read to its end.
// freeBytes is the free space of the disk, -1 when unknown.
// owner, when not nil, says whether a database the MANIFEST names may be the site's by the records of this server.
// key is the backup key of this server (backup.KeyFile); a copy whose MANIFEST is not signed with it is refused.
func FetchBackup(ctx context.Context, r Remote, siteName, stamp, siteDir string, freeBytes int64, log io.Writer, owner func(db string) bool, key []byte) error {
	if !site.ValidName(siteName) {
		return fmt.Errorf("invalid site name %q", siteName)
	}
	if !backup.ValidStamp(stamp) {
		return fmt.Errorf("invalid backup name %q", stamp)
	}
	final := filepath.Join(siteDir, stamp)
	if _, err := os.Stat(final); err == nil {
		return fmt.Errorf("cloud-exists: the backup %s is on this server already", stamp)
	}
	dir := siteName + "/" + stamp
	inside, err := r.List(ctx, dir)
	if err != nil {
		return err
	}
	files := backupFiles(inside)
	missing := fmt.Errorf("cloud-nocopy: the storage has no finished copy of %s", dir)
	manifest, ok := files["MANIFEST"]
	if !ok {
		return missing
	}
	if err := os.MkdirAll(siteDir, 0o700); err != nil {
		return err
	}
	tmp := filepath.Join(siteDir, ".dl-"+stamp)
	if err := os.RemoveAll(tmp); err != nil {
		return err
	}
	if err := os.Mkdir(tmp, 0o700); err != nil {
		return err
	}
	done := false
	defer func() {
		if !done {
			_ = os.RemoveAll(tmp)
		}
	}()

	if manifest.Size > 64<<10 {
		return fmt.Errorf("the MANIFEST in the storage is %d bytes, too big to be one", manifest.Size)
	}
	if err := download(ctx, r, dir, manifest, tmp, log); err != nil {
		return err
	}
	text, err := os.ReadFile(filepath.Join(tmp, "MANIFEST"))
	if err != nil {
		return err
	}
	m, err := backup.ParseManifest(string(text), key)
	if err != nil {
		return fmt.Errorf("the MANIFEST in the storage is not valid: %w", err)
	}
	// The storage is somebody else's disk: whoever can write to it could put a backup there that this server would restore as root.
	// A MANIFEST signed with this server's key was made here, and it carries the SHA-256 of every file it lists.
	if !m.Signed {
		return fmt.Errorf("cloud-unsigned: the copy %s was not made by this server (its MANIFEST has no signature of this server's key)", dir)
	}
	if m.Site != siteName || m.Stamp != stamp {
		return fmt.Errorf("the MANIFEST in the storage is for %s/%s, not %s", m.Site, m.Stamp, dir)
	}
	var want []string
	if m.Files {
		want = append(want, "files.tar.gz")
	}
	for _, db := range m.DBs {
		// The name must fit the site, and when this server has a record of the database, the record must be the site's: a copy in the
		// storage, which somebody else may have written, cannot bring in a dump of another site's database.
		if !dbadmin.ValidName(db) || !dbadmin.OwnedBy(db, siteName) || (owner != nil && !owner(db)) {
			return fmt.Errorf("the MANIFEST in the storage names the database %q, which is not a database of the site %q", db, siteName)
		}
		want = append(want, db+".sql.gz")
	}
	var total int64
	for _, n := range want {
		f, ok := files[n]
		if !ok {
			return fmt.Errorf("cloud-nocopy: the copy in the storage lacks %s, which its MANIFEST lists", n)
		}
		total += f.Size
	}
	if need := total + total/10 + 64<<20; freeBytes >= 0 && need > freeBytes {
		return fmt.Errorf("cloud-nospace: the backup needs about %d MB and this server has %d MB free", need>>20, freeBytes>>20)
	}
	for _, n := range want {
		if err := download(ctx, r, dir, files[n], tmp, log); err != nil {
			return err
		}
		if err := backup.CheckFileSum(m, filepath.Join(tmp, n)); err != nil {
			return fmt.Errorf("%s in the storage is not the file this server backed up: %w", n, err)
		}
	}
	if err := os.Rename(tmp, final); err != nil {
		return err
	}
	done = true
	return nil
}
