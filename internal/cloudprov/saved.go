package cloudprov

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Paths of the files root keeps for a storage connected through rclone. rclone.conf holds the secrets, so only root reads it;
// remote.json holds no secret. They live here rather than in internal/remote so that a program without rclone (mpn itself; the
// copy runs in mpn-cloud) can tell whether a storage is connected and show it.
const (
	RcloneConfigFile = "/etc/mpn/rclone.conf"
	SettingsFile     = "/etc/mpn/remote.json"
)

// Saved is what remote.json holds: enough for the panel to show the connection and fill the form again, never a secret.
type Saved struct {
	Type   string            `json:"type"`
	Bucket string            `json:"bucket,omitempty"`
	Folder string            `json:"folder,omitempty"`
	Shown  map[string]string `json:"shown,omitempty"`
}

func (s Saved) Root() string { return Root(s.Bucket, s.Folder) }

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
	if _, ok := Find(s.Type); !ok {
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
