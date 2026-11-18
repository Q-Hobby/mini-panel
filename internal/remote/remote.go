// Package remote is the copy of backups to storage outside the server through rclone, imported as a Go library. Everything mpn takes
// from rclone is in this package, so a change of the rclone API is mended in one place.
//
// rclone keeps global state (its configuration, its registry, its counters), so this package is used only by the short-lived
// "mpn cloud ..." process that runs as root, never by "mpn web" or "mpn root".
package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/config"
	"github.com/rclone/rclone/fs/config/configfile"
	"github.com/rclone/rclone/fs/config/obscure"
	"github.com/rclone/rclone/fs/operations"

	"mpn/internal/cloudprov"
	"mpn/internal/cloudsync"
)

// Paths of the files root keeps. rclone.conf holds the secrets of the storage, so only root reads it; remote.json holds no secret.
const (
	ConfigFile   = "/etc/mpn/rclone.conf"
	SettingsFile = "/etc/mpn/remote.json"
	RemoteName   = "mpn"
)

// Saved is what remote.json holds: enough for the panel to show the connection and fill the form again, never a secret.
type Saved struct {
	Type   string            `json:"type"`
	Bucket string            `json:"bucket,omitempty"`
	Folder string            `json:"folder,omitempty"`
	Shown  map[string]string `json:"shown,omitempty"`
}

func (s Saved) Root() string { return cloudprov.Root(s.Bucket, s.Folder) }

// LoadSaved returns the connection, or an error that wraps os.ErrNotExist when there is none.
func LoadSaved(path string) (Saved, error) {
	var s Saved
	b, err := os.ReadFile(path)
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return s, fmt.Errorf("%s: %w", path, err)
	}
	if _, ok := cloudprov.Find(s.Type); !ok {
		return s, fmt.Errorf("%s names an unknown storage type %q", path, s.Type)
	}
	return s, nil
}

// WritePrivate writes a file that only root reads, whole or not at all.
func WritePrivate(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".remote-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Setup points rclone at its config file and keeps it small for a 1 GB server: one file at a time, little buffer.
func Setup(ctx context.Context, configPath string) (context.Context, error) {
	if err := config.SetConfigPath(configPath); err != nil {
		return ctx, err
	}
	configfile.Install()
	ctx, ci := fs.AddConfig(ctx)
	ci.Transfers = 1
	ci.Checkers = 2
	ci.BufferSize = 16 << 20
	ci.LowLevelRetries = 10
	ci.LogLevel = fs.LogLevelNotice
	return ctx, nil
}

// Write replaces the remote "mpn" in the config file rclone was pointed at with the checked settings, and saves remote.json beside it.
// Secrets are obscured the way rclone reads them; the config file is 0600.
func Write(s cloudprov.Settings, settingsPath string) error {
	params, err := s.Check()
	if err != nil {
		return err
	}
	p, _ := cloudprov.Find(s.Type)
	configPath := config.GetConfigPath()
	if _, err := os.Stat(configPath); errors.Is(err, os.ErrNotExist) {
		if err := WritePrivate(configPath, nil); err != nil { // rclone keeps the mode of the file it replaces
			return err
		}
	}
	data := config.LoadedData()
	for _, name := range data.GetSectionList() { // the panel owns this file: only its one remote is kept
		data.DeleteSection(name)
	}
	data.SetValue(RemoteName, "type", s.Type)
	secret, err := obscuredKeys(s.Type)
	if err != nil {
		return err
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := params[k]
		if v == "" {
			continue
		}
		if secret[k] {
			if v, err = obscure.Obscure(v); err != nil {
				return err
			}
		}
		data.SetValue(RemoteName, k, v)
	}
	if err := data.Save(); err != nil {
		return err
	}
	if err := os.Chmod(configPath, 0o600); err != nil {
		return err
	}
	b, _ := json.Marshal(Saved{Type: s.Type, Bucket: s.Bucket, Folder: s.Folder, Shown: p.Shown(params)})
	return WritePrivate(settingsPath, b)
}

// obscuredKeys lists the options of a backend that rclone keeps obscured in its config file: the ones it marks IsPassword. Others,
// such as the secret key of S3 or the application key of B2, are read as they are, so obscuring them breaks the login. The answer
// comes from rclone itself rather than from the form, so the two cannot drift apart.
func obscuredKeys(typ string) (map[string]bool, error) {
	ri, err := fs.Find(typ)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, o := range ri.Options {
		if o.IsPassword {
			out[o.Name] = true
		}
	}
	return out, nil
}

// checkKeys refuses a remote that holds a key the panel would not write. A storage with a form made by hand holds only its own keys.
// A generated one may also hold keys rclone writes itself while it runs (a refreshed token, the drive it found, a session), so any
// option of the backend is accepted there as long as it is not Dangerous; a key that is not an option of rclone at all is refused.
func checkKeys(p cloudprov.Provider, keys []string) error {
	allowed := p.Allowed()
	var known map[string]bool
	if p.Generated {
		known = map[string]bool{}
		if ri, err := fs.Find(p.Type); err == nil {
			for _, o := range ri.Options {
				known[o.Name] = true
			}
		}
	}
	for _, k := range keys {
		if allowed[k] || p.Generated && known[k] && !cloudprov.Dangerous(k) {
			continue
		}
		return fmt.Errorf("%s has the setting %q, which mpn does not accept for %s; connect the storage again from the panel", config.GetConfigPath(), k, p.Type)
	}
	return nil
}

// Remote is the folder mpn-backups in the storage. It satisfies cloudsync.Remote.
type Remote struct {
	f fs.Fs
}

var _ cloudsync.Remote = (*Remote)(nil)

// ErrNotConnected means no storage is connected.
var ErrNotConnected = errors.New("cloud-not-connected: no storage is connected")

// Open opens the folder mpn-backups of the remote "mpn". The remote must hold only keys the panel writes: a remote of a type that is not
// on the list, or with any other key, is refused, so a config file changed by hand cannot make root run a command or read a file.
func Open(ctx context.Context, root string) (*Remote, error) {
	data := config.LoadedData()
	if !data.HasSection(RemoteName) {
		return nil, ErrNotConnected
	}
	typ, _ := data.GetValue(RemoteName, "type")
	p, ok := cloudprov.Find(typ)
	if !ok {
		return nil, fmt.Errorf("%s has a remote of the type %q, which mpn does not use; connect the storage again from the panel", config.GetConfigPath(), typ)
	}
	if err := checkKeys(p, data.GetKeyList(RemoteName)); err != nil {
		return nil, err
	}
	f, err := fs.NewFs(ctx, RemoteName+":"+root)
	if err != nil && !errors.Is(err, fs.ErrorIsFile) {
		return nil, err
	}
	return &Remote{f: f}, nil
}

// List returns what is directly in dir ("" is mpn-backups itself). A folder that does not exist is empty.
func (r *Remote) List(ctx context.Context, dir string) ([]cloudsync.Entry, error) {
	entries, err := r.f.List(ctx, dir)
	if errors.Is(err, fs.ErrorDirNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := make([]cloudsync.Entry, 0, len(entries))
	for _, e := range entries {
		switch x := e.(type) {
		case fs.Directory:
			out = append(out, cloudsync.Entry{Name: path.Base(x.Remote()), Dir: true})
		case fs.Object:
			out = append(out, cloudsync.Entry{Name: path.Base(x.Remote()), Size: x.Size()})
		}
	}
	return out, nil
}

// Upload copies a local file to dir/name. rclone compares a hash after the copy when both sides have one, and the size always.
func (r *Remote) Upload(ctx context.Context, dir, name, localPath string) error {
	src, err := fs.NewFs(ctx, filepath.Dir(localPath))
	if err != nil {
		return err
	}
	return operations.CopyFile(ctx, r.f, src, path.Join(dir, name), filepath.Base(localPath))
}

// Download copies a file of the storage to localPath and makes it 0600. rclone compares the hash when the storage has one.
func (r *Remote) Download(ctx context.Context, remotePath, localPath string) error {
	dst, err := fs.NewFs(ctx, filepath.Dir(localPath))
	if err != nil {
		return err
	}
	if err := operations.CopyFile(ctx, dst, r.f, filepath.Base(localPath), remotePath); err != nil {
		return err
	}
	return os.Chmod(localPath, 0o600)
}

// Purge removes dir and everything in it. A folder that is already gone is not an error.
func (r *Remote) Purge(ctx context.Context, dir string) error {
	err := operations.Purge(ctx, r.f, dir)
	if errors.Is(err, fs.ErrorDirNotFound) {
		return nil
	}
	return err
}

// Mkdir makes dir. Storage without real folders (S3, B2) makes nothing.
func (r *Remote) Mkdir(ctx context.Context, dir string) error { return operations.Mkdir(ctx, r.f, dir) }

// Test writes, reads back and deletes a small file in mpn-backups, so a connection counts only once it works.
func (r *Remote) Test(ctx context.Context, scratch string) error {
	if err := r.Mkdir(ctx, ""); err != nil {
		return fmt.Errorf("cloud-test-write: %w", err)
	}
	name := fmt.Sprintf(".mpn-test-%d", os.Getpid())
	local := filepath.Join(scratch, name)
	want := "mpn " + name + "\n"
	if err := os.WriteFile(local, []byte(want), 0o600); err != nil {
		return err
	}
	defer os.Remove(local)
	if err := r.Upload(ctx, "", name, local); err != nil {
		return fmt.Errorf("cloud-test-write: %w", err)
	}
	back := local + ".back"
	defer os.Remove(back)
	if err := r.Download(ctx, name, back); err != nil {
		return fmt.Errorf("cloud-test-read: %w", err)
	}
	if got, err := os.ReadFile(back); err != nil || string(got) != want {
		return errors.New("cloud-test-read: the file read back is not the one written")
	}
	obj, err := r.f.NewObject(ctx, name)
	if err != nil {
		return fmt.Errorf("cloud-test-delete: %w", err)
	}
	if err := operations.DeleteFile(ctx, obj); err != nil {
		return fmt.Errorf("cloud-test-delete: %w", err)
	}
	return nil
}
