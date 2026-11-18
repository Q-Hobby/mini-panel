// Package safedir makes folders inside a tree that someone else owns, as root, without ever following a link.
//
// The web root of a site lives in the home of the site's user, who can rename, delete and replace anything there at any
// moment. A command that root runs on a path in that tree (install -d, chmod, chown) looks the path up again each time,
// so the user can put a symbolic link in its place between two steps and make root change a file elsewhere. Here every
// step goes through an open folder descriptor and refuses links, so nothing outside the tree can be reached.
package safedir

import (
	"errors"
	"strings"
)

// ErrLink means a name on the way is a symbolic link, or a file that is not a folder. The user put it there, and MkdirAll
// leaves such a tree as it is (as the shell steps did before: "[ ! -e P ] && [ ! -L P ]").
var ErrLink = errors.New("a name on the way is a symbolic link or not a folder")

// validElem reports whether el is one plain name: not empty, no "/", not "." or "..".
func validElem(el string) bool {
	return el != "" && el != "." && el != ".." && !strings.ContainsRune(el, '/') && !strings.ContainsRune(el, 0)
}

// Split turns "htdocs/app/public" into its names and checks each one.
func Split(rel string) ([]string, error) {
	if rel == "" {
		return nil, errors.New("no folder given")
	}
	els := strings.Split(rel, "/")
	for _, el := range els {
		if !validElem(el) {
			return nil, errors.New("folder " + rel + " has a name that is not allowed")
		}
	}
	return els, nil
}
