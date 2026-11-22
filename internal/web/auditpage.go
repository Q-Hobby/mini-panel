package web

import (
	"encoding/csv"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"mpn/internal/audit"
	"mpn/internal/ctl"
	"mpn/internal/site"
	"mpn/internal/webauth"
)

// auditPageSize is how many entries one page of the activity log shows; an export takes up to audit.MaxPage.
const auditPageSize = 50

// auditView is what the activity log draws, on its own page and in the tab of a website.
type auditView struct {
	Filter           audit.Filter
	Kinds            []string
	Page             audit.Page
	Rows             []auditRow
	PageNo, Pages    int
	PrevURL, NextURL string
	CSVURL           string
	Action           string // where the filter form goes
	Error            string
}

// auditRow is an entry as the page shows it.
type auditRow struct {
	audit.Entry
	When, Action string
}

// auditFilter reads the filter from the address. A value that is not valid is dropped, and the page says so.
func auditFilter(q url.Values) (audit.Filter, bool) {
	f := audit.Filter{Site: q.Get("site"), IP: q.Get("ip"), Kind: q.Get("kind"), From: q.Get("from"), To: q.Get("to")}
	ok := true
	if f.Site != "" && !site.ValidName(f.Site) {
		f.Site, ok = "", false
	}
	if audit.ValidIPFilter(f.IP) != nil {
		f.IP, ok = "", false
	}
	if audit.ValidKind(f.Kind) != nil {
		f.Kind, ok = "", false
	}
	if audit.ValidDay(f.From) != nil {
		f.From, ok = "", false
	}
	if audit.ValidDay(f.To) != nil {
		f.To, ok = "", false
	}
	return f, ok
}

// auditQuery is the address of the log with a filter and a page.
func auditQuery(base string, f audit.Filter, page int, withSite bool) string {
	v := url.Values{}
	if withSite && f.Site != "" {
		v.Set("site", f.Site)
	}
	for k, x := range map[string]string{"ip": f.IP, "kind": f.Kind, "from": f.From, "to": f.To} {
		if x != "" {
			v.Set(k, x)
		}
	}
	if page > 1 {
		v.Set("page", strconv.Itoa(page))
	}
	if len(v) == 0 {
		return base
	}
	return base + "?" + v.Encode()
}

// loadAudit reads one page of the log. base is the address of the page (/activity or the tab of a site), siteName the website the tab
// belongs to ("" on the page of all of them).
func (s *Server) loadAudit(w http.ResponseWriter, r *http.Request, base, siteName string) *auditView {
	T := translatorForWriter(w)
	f, ok := auditFilter(r.URL.Query())
	if siteName != "" {
		f.Site = siteName
	}
	v := &auditView{Filter: f, Kinds: audit.Kinds, Action: base}
	if !ok {
		v.Error = T("audit.err.filter")
	}
	pageNo, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if pageNo < 1 || pageNo > 10000 {
		pageNo = 1
	}
	req := ctl.Request{Op: "audit.list", Site: f.Site, AuditIP: f.IP, AuditKind: f.Kind, From: f.From, To: f.To, Limit: auditPageSize, Offset: (pageNo - 1) * auditPageSize}
	if e := s.listText(T, req, &v.Page); e != "" {
		v.Error = e
		return v
	}
	v.PageNo, v.Pages = pageNo, (v.Page.Total+auditPageSize-1)/auditPageSize
	withSite := siteName == ""
	if pageNo > 1 {
		v.PrevURL = auditQuery(base, f, pageNo-1, withSite)
	}
	if pageNo < v.Pages {
		v.NextURL = auditQuery(base, f, pageNo+1, withSite)
	}
	v.CSVURL = auditQuery("/activity.csv", f, 0, true)
	for _, e := range v.Page.Entries {
		v.Rows = append(v.Rows, auditRow{Entry: e, When: time.Unix(e.At, 0).Format("02/01/2006 15:04:05"), Action: audit.OpName(writerLanguage(w), e.Op)})
	}
	return v
}

func (s *Server) auditPages(mux *http.ServeMux) {
	mux.HandleFunc("GET /activity", s.auth(s.auditPage))
	mux.HandleFunc("GET /activity.csv", s.auth(s.auditCSV))
}

func (s *Server) auditPage(w http.ResponseWriter, r *http.Request, ss webauth.Session) {
	T := translatorForWriter(w)
	v := view{Page: "activity", Audit: s.loadAudit(w, r, "/activity", "")}
	_ = s.listText(T, ctl.Request{Op: "site.list"}, &v.Sites)
	s.show(w, 200, ss, v)
}

// auditCSV gives the entries the filter chooses as a file, up to audit.MaxPage of them, newest first.
func (s *Server) auditCSV(w http.ResponseWriter, r *http.Request, ss webauth.Session) {
	T := translatorForWriter(w)
	f, _ := auditFilter(r.URL.Query())
	var page audit.Page
	if e := s.listText(T, ctl.Request{Op: "audit.list", Site: f.Site, AuditIP: f.IP, AuditKind: f.Kind, From: f.From, To: f.To, Limit: audit.MaxPage}, &page); e != "" {
		http.Error(w, e, http.StatusBadGateway)
		return
	}
	name := "mpn-activity"
	if f.Site != "" {
		name += "-" + f.Site
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+"-"+time.Now().Format("20060102")+`.csv"`)
	_, _ = w.Write([]byte("\xEF\xBB\xBF")) // so a spreadsheet reads the file as UTF-8
	cw := csv.NewWriter(w)
	_ = cw.Write(audit.CSVHeader)
	for _, e := range page.Entries {
		_ = cw.Write(audit.CSVRow(e, time.Local))
	}
	cw.Flush()
}
