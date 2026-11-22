package web

import (
	"net/http"
	"path"
	"strings"

	"mpn/internal/ctl"
)

// note tells the root helper about something only the web side sees (a sign-in, the terminal, the file manager), for the activity
// log. It never fails the page: a log that cannot be written is the helper's business.
func (s *Server) note(r *http.Request, event, site, detail string, failed bool) {
	s.noteFrom(s.clientAddr(r), event, site, detail, failed)
}

func (s *Server) noteFrom(addr, event, site, detail string, failed bool) {
	if s.Call == nil {
		return
	}
	_, _ = s.Call(ctl.Request{Op: "audit.note", Actor: addr, Event: event, Site: site, Note: detail, Failed: failed})
}

// fileEvents maps the last part of the address of an action of the file manager to its event.
var fileEvents = map[string]string{
	"upload": "files.upload", "mkdir": "files.mkdir", "newfile": "files.newfile", "rename": "files.rename", "delete": "files.delete",
	"zip": "files.zip", "unzip": "files.unzip", "chmod": "files.chmod", "paste": "files.paste", "save": "files.save",
}

// noteFiles writes an action of the file manager to the log: the folder, the names it acted on and the outcome. The content of a
// file is never part of it.
func (s *Server) noteFiles(r *http.Request, site, dir string, res *result, extra string) {
	event, ok := fileEvents[path.Base(r.URL.Path)]
	if !ok {
		return
	}
	var p []string
	add := func(k, v string) {
		if v = strings.TrimSpace(v); v != "" {
			if k != "" {
				v = k + "=" + v
			}
			p = append(p, v)
		}
	}
	add("dir", "/"+dir)
	if r.PostForm != nil { // an upload is read as a stream and has no form values
		if event == "files.paste" {
			event = "files.copy"
			if r.PostForm.Get("mode") == "cut" {
				event = "files.move"
			}
			add("from", "/"+strings.TrimPrefix(r.PostForm.Get("from"), "/"))
		}
		items := r.PostForm["item"]
		if v := r.PostForm.Get("items"); v != "" {
			items = append(items, strings.Split(v, "\n")...)
		}
		if v := r.PostForm.Get("path"); v != "" && event != "files.save" {
			items = append(items, v)
		}
		add("items", strings.Join(items, ","))
		for _, k := range []string{"name", "newname", "zipname", "zip", "mode"} {
			if k == "mode" && event != "files.chmod" {
				continue
			}
			add(k, r.PostForm.Get(k))
		}
	}
	add("", extra)
	if res != nil && !res.OK {
		add("error", res.Error)
	}
	s.note(r, event, site, strings.Join(p, " "), res != nil && !res.OK)
}

// auditUser is the name typed at a failed sign-in as the log keeps it: the name of the account when it was that, and "other"
// otherwise, because a person who types the password into the wrong box would leave it in the log.
func auditUser(typed, account string) string {
	if typed != "" && typed == account {
		return typed
	}
	return "other"
}

// actorKey holds the address of the person in the context of a terminal, which outlives its request.
type actorKey struct{}
