//go:build linux

package safedir

import (
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

const dirFlags = unix.O_RDONLY | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_CLOEXEC

// MkdirAll makes each missing folder of rel below base, gives each new one to uid:gid with the given mode (setgid
// included), and leaves folders that already exist exactly as they are.
//
// base itself must not be a link (it is opened with O_NOFOLLOW; its parent must belong to root, as /home does). When a
// folder on the way is a symbolic link the tree is the user's own arrangement and nothing below it is made: MkdirAll
// returns ErrLink and has changed nothing. A folder that is replaced by a link between being made and being opened is
// a race someone set up on purpose, and is an error.
func MkdirAll(base string, rel []string, uid, gid int, mode uint32) error {
	for _, el := range rel {
		if !validElem(el) {
			return fmt.Errorf("folder name %q is not allowed", el)
		}
	}
	fd, err := unix.Open(base, dirFlags, 0)
	if err != nil {
		return fmt.Errorf("open %s: %w", base, err)
	}
	path := base
	for _, el := range rel {
		path += "/" + el
		next, created, err := step(fd, el)
		unix.Close(fd)
		if err != nil {
			if errors.Is(err, ErrLink) {
				return ErrLink
			}
			return fmt.Errorf("%s: %w", path, err)
		}
		if created {
			// Through the descriptor of the folder just made: a name swapped since cannot redirect these.
			if err := unix.Fchown(next, uid, gid); err != nil {
				unix.Close(next)
				return fmt.Errorf("chown %s: %w", path, err)
			}
			if err := unix.Fchmod(next, mode); err != nil {
				unix.Close(next)
				return fmt.Errorf("chmod %s: %w", path, err)
			}
		}
		fd = next
	}
	unix.Close(fd)
	return nil
}

// step opens the folder el inside the folder dir, making it first when nothing is there.
func step(dir int, el string) (fd int, created bool, err error) {
	fd, err = unix.Openat(dir, el, dirFlags, 0)
	if err == unix.ENOENT {
		// Made with no rights for anybody; the right owner and mode are set through the descriptor below.
		switch merr := unix.Mkdirat(dir, el, 0o700); merr {
		case nil:
			created = true
		case unix.EEXIST: // made by somebody else in between: treated like a folder that was there
		default:
			return -1, false, merr
		}
		fd, err = unix.Openat(dir, el, dirFlags, 0)
	}
	if err == nil {
		return fd, created, nil
	}
	if err == unix.ELOOP || err == unix.ENOTDIR {
		var st unix.Stat_t
		if serr := unix.Fstatat(dir, el, &st, unix.AT_SYMLINK_NOFOLLOW); serr == nil && st.Mode&unix.S_IFMT == unix.S_IFLNK {
			if created {
				return -1, false, errors.New("the new folder was replaced by a symbolic link")
			}
			return -1, false, ErrLink
		}
		if created {
			return -1, false, errors.New("the new folder was replaced by something else")
		}
		return -1, false, ErrLink
	}
	return -1, false, err
}
