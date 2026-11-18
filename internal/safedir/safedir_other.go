//go:build !linux

package safedir

import "errors"

// MkdirAll works only on Linux, where mpn changes machines.
func MkdirAll(base string, rel []string, uid, gid int, mode uint32) error {
	return errors.New("safedir works only on Linux")
}
