package gdrive

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"

	"mpn/internal/cloudsync"
)

// PathRemote shows a folder of Drive as the path-based storage cloudsync works with. Drive finds things by id, so each path is
// resolved one folder at a time from Root. Every file that comes back is compared with the MD5 Google holds.
type PathRemote struct {
	C    *Client
	Root string // the id of the folder mpn-backups
}

var _ cloudsync.Remote = (*PathRemote)(nil)

func split(p string) []string {
	var out []string
	for _, s := range strings.Split(p, "/") {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// find returns the item named name in the folder parent, or ok false.
func (r *PathRemote) find(ctx context.Context, parent, name string) (File, bool, error) {
	list, err := r.C.List(ctx, parent)
	if err != nil {
		return File{}, false, err
	}
	for _, f := range list {
		if f.Name == name {
			return f, true, nil
		}
	}
	return File{}, false, nil
}

// folder returns the id of the folder at p, or "" when it does not exist. With make it makes what is missing.
func (r *PathRemote) folder(ctx context.Context, p string, make bool) (string, error) {
	id := r.Root
	for _, name := range split(p) {
		if make {
			next, err := r.C.EnsureFolder(ctx, id, name)
			if err != nil {
				return "", err
			}
			id = next
			continue
		}
		f, ok, err := r.find(ctx, id, name)
		if err != nil || !ok || !f.IsFolder() {
			return "", err
		}
		id = f.ID
	}
	return id, nil
}

func (r *PathRemote) List(ctx context.Context, dir string) ([]cloudsync.Entry, error) {
	id, err := r.folder(ctx, dir, false)
	if err != nil || id == "" {
		return nil, err
	}
	list, err := r.C.List(ctx, id)
	if err != nil {
		return nil, err
	}
	out := make([]cloudsync.Entry, 0, len(list))
	for _, f := range list {
		out = append(out, cloudsync.Entry{Name: f.Name, Dir: f.IsFolder(), Size: f.Size})
	}
	return out, nil
}

func (r *PathRemote) Mkdir(ctx context.Context, dir string) error {
	_, err := r.folder(ctx, dir, true)
	return err
}

func (r *PathRemote) Upload(ctx context.Context, dir, name, localPath string) error {
	id, err := r.folder(ctx, dir, true)
	if err != nil {
		return err
	}
	_, err = r.C.Upload(ctx, id, name, localPath)
	return err
}

func (r *PathRemote) Download(ctx context.Context, remotePath, localPath string) error {
	parts := split(remotePath)
	if len(parts) == 0 {
		return fmt.Errorf("no file named")
	}
	dir, err := r.folder(ctx, strings.Join(parts[:len(parts)-1], "/"), false)
	if err != nil {
		return err
	}
	if dir == "" {
		return fmt.Errorf("%s is not on the drive", remotePath)
	}
	f, ok, err := r.find(ctx, dir, parts[len(parts)-1])
	if err != nil {
		return err
	}
	if !ok || f.IsFolder() {
		return fmt.Errorf("%s is not on the drive", remotePath)
	}
	if err := r.C.Download(ctx, f.ID, localPath, f.Size); err != nil {
		return err
	}
	if f.MD5 == "" {
		return nil
	}
	h := md5.New()
	in, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer in.Close()
	if _, err := io.Copy(h, in); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != f.MD5 {
		return fmt.Errorf("damaged: its checksum is %s, Google has %s", got, f.MD5)
	}
	return nil
}

func (r *PathRemote) Purge(ctx context.Context, dir string) error {
	id, err := r.folder(ctx, dir, false)
	if err != nil || id == "" || id == r.Root {
		return err
	}
	return r.C.Delete(ctx, id)
}
