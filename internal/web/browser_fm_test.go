package web

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The steps run inside the page, in the browser. Each one records "ok name" or "FAIL name: why" in a <pre>.
const fmSteps = `
document.addEventListener('DOMContentLoaded', function () { // after the page's own deferred scripts have run
  var out = [];
  try { out = JSON.parse(sessionStorage.getItem('out') || '[]'); } catch (_) {}
  function save() { try { sessionStorage.setItem('out', JSON.stringify(out)); } catch (_) {} }
  out.push('info page loaded');
  function check(name, cond, why) { out.push((cond ? 'ok ' : 'FAIL ') + name + (cond ? '' : ': ' + (why || ''))); }
  function $(id) { return document.getElementById(id); }
  function rowsShown() { return Array.prototype.filter.call(document.querySelectorAll('#fm-body tr'), function (r) { return !r.hidden; }); }
  function names() { return rowsShown().map(function (r) { return r.getAttribute('data-name'); }); }
  function click(el) { el.click(); }
  function row(n) { return document.querySelector('#fm-body tr[data-name="' + n + '"]'); }
  function box(n) { return row(n).querySelector('input[name=item]'); }
  function finish() {
    save();
    var pre = document.createElement('pre'); pre.id = 't-result'; pre.textContent = out.join('\n'); document.body.appendChild(pre);
  }
  try {
    var realSend = XMLHttpRequest.prototype.send;
    XMLHttpRequest.prototype.send = function () {
      var x = this;
      x.addEventListener('loadend', function () { out.push('info xhr ended: status ' + x.status + ' ' + String(x.responseText).slice(0, 200)); save(); });
      return realSend.apply(this, arguments);
    };
    if (sessionStorage.getItem('uploaded')) {
      check('page reloaded after the upload and lists the new file', !!row('up.txt'), names().join(','));
      check('no leftover message about the upload', !document.querySelector('[role=status]:not([data-copy-status])'), 'a flash message was left for the next page');
      return finish();
    }
    var total = document.querySelectorAll('#fm-body tr').length;
    check('the list has rows', total > 5, total);
    check('bar for chosen items starts hidden', $('fm-sel').hidden);
    box('notes.txt').click(); box('app.js').click();
    check('choosing shows the bar with the count', !$('fm-sel').hidden && $('fm-count').textContent.indexOf('2') >= 0, $('fm-count').textContent);
    check('a chosen row is marked', row('notes.txt').getAttribute('data-selected') === '1');
    check('select-all becomes indeterminate', $('fm-all').indeterminate === true);
    click(document.querySelector('[data-fm=zip]'));
    check('zip dialog opens', $('dlg-zip').open);
    check('zip dialog carries both names', $('dlg-zip-form').querySelectorAll('input[name=item]').length === 2);
    check('zip dialog suggests a name', /\.zip$/.test($('dlg-zip-name').value), $('dlg-zip-name').value);
    $('dlg-zip').close();
    click(document.querySelector('[data-fm=delete-selected]'));
    check('delete dialog opens with a count', $('dlg-delete').open && $('dlg-delete-title').textContent.indexOf('2') >= 0, $('dlg-delete-title').textContent);
    check('delete dialog lists the names', $('dlg-delete-list').children.length === 2 && $('dlg-delete-items').value.split('\n').length === 2);
    $('dlg-delete').close();
    click(document.querySelector('[data-fm=clear]'));
    check('clearing hides the bar', $('fm-sel').hidden && !box('notes.txt').checked);
    $('fm-all').click();
    check('select-all marks every shown row', rowsShown().every(function (r) { return r.querySelector('input[name=item]').checked; }) && $('fm-count').textContent.indexOf(String(total)) >= 0, $('fm-count').textContent);
    $('fm-all').click();
    check('select-all again clears', $('fm-sel').hidden);

    var f = $('fm-filter'); f.value = 'PHP'; f.dispatchEvent(new Event('input', { bubbles: true }));
    check('filter keeps only matching names, ignoring case', names().length > 0 && names().every(function (n) { return n.toLowerCase().indexOf('php') >= 0; }), names().join(','));
    f.value = 'zzzz'; f.dispatchEvent(new Event('input', { bubbles: true }));
    check('filter with no match shows the notice', rowsShown().length === 0 && !$('fm-nomatch').hidden);
    f.value = ''; f.dispatchEvent(new Event('input', { bubbles: true }));
    check('empty filter shows everything again', rowsShown().length === total && $('fm-nomatch').hidden);

    var sizeBtn = document.querySelector('[data-sort=size]');
    click(sizeBtn);
    var order = Array.prototype.slice.call(document.querySelectorAll('#fm-body tr'));
    var dirsFirst = true, seenFile = false;
    order.forEach(function (r) { if (r.getAttribute('data-dir') === '0') seenFile = true; else if (seenFile) dirsFirst = false; });
    var sizes = order.filter(function (r) { return r.getAttribute('data-dir') === '0'; }).map(function (r) { return Number(r.getAttribute('data-size')); });
    check('sorting by size puts folders first and sizes ascending', dirsFirst && sizes.every(function (s, i) { return i === 0 || sizes[i - 1] <= s; }), sizes.join(','));
    click(sizeBtn);
    sizes = Array.prototype.filter.call(document.querySelectorAll('#fm-body tr'), function (r) { return r.getAttribute('data-dir') === '0'; }).map(function (r) { return Number(r.getAttribute('data-size')); });
    check('a second click reverses the order', sizes.every(function (s, i) { return i === 0 || sizes[i - 1] >= s; }), sizes.join(','));

    click(document.querySelector('[data-fm=newdir]'));
    check('new folder dialog opens and posts to mkdir', $('dlg-name').open && /\/files\/mkdir$/.test($('dlg-name-form').getAttribute('action')), $('dlg-name-form').getAttribute('action'));
    $('dlg-name').close();
    click(document.querySelector('[data-fm=newfile]'));
    check('new file dialog posts to newfile', /\/files\/newfile$/.test($('dlg-name-form').getAttribute('action')));
    $('dlg-name').close();
    click(document.querySelector('[data-fm=rename][data-name="notes.txt"]'));
    check('rename dialog is prefilled and knows the path', $('dlg-name-input').value === 'notes.txt' && $('dlg-name-path').value === 'htdocs/notes.txt' && /\/files\/rename$/.test($('dlg-name-form').getAttribute('action')), $('dlg-name-input').value + ' | ' + $('dlg-name-path').value);
    $('dlg-name').close();
    click(document.querySelector('[data-fm=delete][data-name="logo.png"]'));
    check('single delete names the file', $('dlg-delete').open && $('dlg-delete-title').textContent.indexOf('logo.png') >= 0 && $('dlg-delete-items').value === 'logo.png', $('dlg-delete-title').textContent);
    $('dlg-delete').close();
    click(document.querySelector('[data-fm=unzip]'));
    check('unzip dialog knows the archive', $('dlg-unzip').open && /\.zip$/.test($('dlg-unzip-zip').value), $('dlg-unzip-zip').value);
    $('dlg-unzip').close();
    check('the page needs no inline style or script', !document.querySelector('[style]'), (document.querySelector('[style]') || {}).outerHTML);

    // the menu of the right mouse button
    var menu = $('fm-menu');
    function offered() { return Array.prototype.filter.call(menu.querySelectorAll('.fm-mi'), function (i) { return !i.hidden; }).map(function (i) { return i.getAttribute('data-m'); }); }
    function rightClick(el) {
      var ev = new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 200, clientY: 200 });
      el.dispatchEvent(ev);
      return ev.defaultPrevented;
    }
    function key(k) { document.body.dispatchEvent(new KeyboardEvent('keydown', { key: k, bubbles: true, cancelable: true })); }
    check('the menu starts hidden', menu.hidden);
    check('right click on a file takes over from the browser menu', rightClick(row('notes.txt').children[1]) === true);
    check('the menu is shown', !menu.hidden);
    check('right click chooses just that file', chosen1() === 'notes.txt', JSON.stringify(Array.prototype.map.call(document.querySelectorAll('#fm-body input:checked'), function (i) { return i.value; })));
    function chosen1() { var c = document.querySelectorAll('#fm-body input[name=item]:checked'); return c.length === 1 ? c[0].value : c.length; }
    var o = offered();
    check('a text file offers edit, download, rename, permissions, zip and delete', ['edit', 'download', 'rename', 'chmod', 'zip', 'delete'].every(function (a) { return o.indexOf(a) >= 0; }), o.join(','));
    check('a text file does not offer open, unzip or the page actions', ['open', 'unzip', 'upload', 'newfile', 'refresh'].every(function (a) { return o.indexOf(a) < 0; }), o.join(','));
    check('the menu stays inside the window', menu.getBoundingClientRect().right <= window.innerWidth && menu.getBoundingClientRect().bottom <= window.innerHeight);
    key('Escape');
    check('Escape closes the menu', menu.hidden);
    rightClick(row('sub').children[1]);
    o = offered();
    check('a folder offers open but not edit or download', o.indexOf('open') >= 0 && o.indexOf('edit') < 0 && o.indexOf('download') < 0, o.join(','));
    document.body.click();
    check('a click elsewhere closes the menu', menu.hidden);
    rightClick(row('b.zip').children[1]);
    check('an archive offers unzip', offered().indexOf('unzip') >= 0, offered().join(','));
    click(menu.querySelector('[data-m=unzip]'));
    check('choosing unzip from the menu opens the dialog and closes the menu', $('dlg-unzip').open && menu.hidden && $('dlg-unzip-zip').value === 'b.zip');
    $('dlg-unzip').close();
    rightClick(row('logo.png').children[1]);
    check('an image is not offered for editing', offered().indexOf('edit') < 0 && offered().indexOf('download') >= 0, offered().join(','));
    box('app.js').click();
    rightClick(row('app.js').children[1]);
    check('right click on a chosen item keeps the whole choice', document.querySelectorAll('#fm-body input[name=item]:checked').length === 2);
    o = offered();
    check('several items offer cut, copy, zip, permissions and delete', o.slice().sort().join(',') === 'chmod,copy,cut,delete,zip', o.join(','));
    key('Escape'); key('Escape');
    rightClick($('fm-bar'));
    o = offered();
    check('right click on the empty part offers the page actions', ['upload', 'newfile', 'newdir', 'refresh', 'selectall'].every(function (a) { return o.indexOf(a) >= 0; }) && o.indexOf('delete') < 0, o.join(','));
    click(menu.querySelector('[data-m=newfile]'));
    check('new file from the menu opens the dialog', $('dlg-name').open && /newfile$/.test($('dlg-name-form').getAttribute('action')));
    $('dlg-name').close();
    check('the page offers no menu over the filter box', rightClick($('fm-filter')) === false);
    check('and the menu stays closed there', menu.hidden);

    // permissions
    rightClick(row('notes.txt').children[1]);
    click(menu.querySelector('[data-m=chmod]'));
    check('permissions dialog opens from the menu', $('dlg-chmod').open && menu.hidden);
    check('it starts from the current mode', $('chmod-octal').value === row('notes.txt').getAttribute('data-mode'), $('chmod-octal').value + ' vs ' + row('notes.txt').getAttribute('data-mode'));
    check('the boxes agree with the number', Array.prototype.map.call(document.querySelectorAll('#dlg-chmod [data-bit]'), function (b) { return b.checked ? b.getAttribute('data-bit') : 0; }).reduce(function (a, b) { return a + parseInt(b, 8); }, 0) === parseInt($('chmod-octal').value, 8));
    check('recursion is not offered for a plain file', $('chmod-rec').hidden);
    $('chmod-octal').value = '750'; $('chmod-octal').dispatchEvent(new Event('input', { bubbles: true }));
    check('typing a number sets the boxes', document.querySelector('#dlg-chmod [data-bit="100"]').checked && document.querySelector('#dlg-chmod [data-bit="10"]').checked && !document.querySelector('#dlg-chmod [data-bit="2"]').checked);
    var w = document.querySelector('#dlg-chmod [data-bit="2"]'); w.checked = true; w.dispatchEvent(new Event('change', { bubbles: true }));
    check('ticking a box sets the number', $('chmod-octal').value === '752', $('chmod-octal').value);
    click(document.querySelector('[data-preset="644"]'));
    check('a preset sets number and boxes', $('chmod-octal').value === '644' && document.querySelector('#dlg-chmod [data-bit="200"]').checked && !document.querySelector('#dlg-chmod [data-bit="100"]').checked);
    check('the form sends the file name', $('dlg-chmod-form').querySelector('input[name=item]').value === 'notes.txt' && /\/files\/chmod$/.test($('dlg-chmod-form').getAttribute('action')));
    $('dlg-chmod').close();
    rightClick(row('sub').children[1]);
    click(menu.querySelector('[data-m=chmod]'));
    check('recursion is offered for a folder, off by default', !$('chmod-rec').hidden && !$('chmod-rec-box').checked && $('chmod-scope').disabled);
    $('chmod-rec-box').click();
    check('ticking recursion enables the choice of kinds', !$('chmod-scope').disabled);
    $('dlg-chmod').close();
    box('notes.txt').checked = false; box('app.js').checked = false; box('sub').checked = false;
    document.querySelectorAll('#fm-body input[name=item]').forEach(function (c) { c.checked = false; });
    box('wp.php').click(); box('wp.php').dispatchEvent(new Event('change', { bubbles: true }));
    key('F2');
    check('F2 renames the chosen item', $('dlg-name').open && $('dlg-name-input').value === 'wp.php', $('dlg-name-input').value);
    $('dlg-name').close();
    key('Delete');
    check('Delete asks to delete the chosen item', $('dlg-delete').open && $('dlg-delete-items').value === 'wp.php');
    $('dlg-delete').close();
    document.querySelectorAll('#fm-body input[name=item]').forEach(function (c) { c.checked = false; });
    box('wp.php').dispatchEvent(new Event('change', { bubbles: true }));

    // cut, copy and paste
    sessionStorage.removeItem('mpn-fm-clip:blog');
    rightClick(row('notes.txt').children[1]);
    o = offered();
    check('a file offers cut and copy but not paste while nothing is cut', o.indexOf('cut') >= 0 && o.indexOf('copy') >= 0 && o.indexOf('paste') < 0, o.join(','));
    click(menu.querySelector('[data-m=copy]'));
    check('copy shows the clipboard bar', !$('fm-clip').hidden && $('fm-clip-text').textContent.indexOf('1') >= 0, $('fm-clip-text').textContent);
    check('copy is kept for the site', JSON.parse(sessionStorage.getItem('mpn-fm-clip:blog')).mode === 'copy');
    rightClick($('fm-bar'));
    check('the empty part now offers paste', offered().indexOf('paste') >= 0, offered().join(','));
    key('Escape');
    rightClick(row('sub').children[1]);
    check('a folder offers paste into it', offered().indexOf('paste') >= 0, offered().join(','));
    key('Escape'); key('Escape');
    document.querySelectorAll('#fm-body input[name=item]').forEach(function (c) { c.checked = false; });
    box('app.js').click(); box('wp.php').click();
    document.body.dispatchEvent(new KeyboardEvent('keydown', { key: 'x', ctrlKey: true, bubbles: true, cancelable: true }));
    var cl = JSON.parse(sessionStorage.getItem('mpn-fm-clip:blog') || '{}');
    check('Ctrl+X cuts the chosen items', cl.mode === 'cut' && cl.names.length === 2 && cl.dir === 'htdocs', JSON.stringify(cl));
    check('cut rows are dimmed', row('app.js').getAttribute('data-cut') === '1' && row('wp.php').getAttribute('data-cut') === '1' && !row('notes.txt').hasAttribute('data-cut'));
    click(document.querySelector('[data-fm=clipclear]'));
    check('cancel empties the clipboard', $('fm-clip').hidden && !sessionStorage.getItem('mpn-fm-clip:blog') && !row('app.js').hasAttribute('data-cut'));
    document.querySelectorAll('#fm-body input[name=item]').forEach(function (c) { c.checked = false; });
    box('app.js').dispatchEvent(new Event('change', { bubbles: true }));
    check('the selection bar has cut and copy', !!document.querySelector('#fm-sel [data-fm=cut]') && !!document.querySelector('#fm-sel [data-fm=copy]'));
    // an upload by choosing a file: the page must send it, show the result and reload
    click(document.querySelector('[data-fm=upload]'));
    check('upload dialog opens', $('dlg-upload').open);
    var dt = new DataTransfer(); dt.items.add(new File(['hello upload'], 'up.txt', { type: 'text/plain' }));
    $('up-input').files = dt.files;
    sessionStorage.setItem('uploaded', '1');
    $('up-input').dispatchEvent(new Event('change'));
    check('upload shows the progress area', !$('up-run').hidden);
    finish();
  } catch (e) { out.push('FAIL script error: ' + e.message); finish(); }
});
`

// TestFileManagerScriptInARealBrowser runs the page's script in headless Edge or Chrome and checks what it does. It runs
// only when MPN_BROWSER is set.
func TestFileManagerScriptInARealBrowser(t *testing.T) {
	browser := os.Getenv("MPN_BROWSER")
	if browser == "" {
		t.Skip("set MPN_BROWSER to the path of Edge or Chrome to run this")
	}
	e := startFM(t)
	for name, body := range map[string]string{"htdocs/index.php": "<?php\n", "htdocs/app.js": "x", "htdocs/notes.txt": "hello", "htdocs/logo.png": "png",
		"htdocs/wp.php": "<?php //\n", "htdocs/big.log": strings.Repeat("l", 3000), "htdocs/b.zip": "zip", "htdocs/sub/x.txt": "x", "htdocs/uploads/y.txt": "y"} {
		p := filepath.Join(e.home, filepath.FromSlash(name))
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte(body), 0o644)
	}
	target, _ := url.Parse(e.url)
	px := httputil.NewSingleHostReverseProxy(target)
	px.ModifyResponse = func(r *http.Response) error {
		if strings.HasPrefix(r.Header.Get("Content-Type"), "text/html") {
			b, _ := io.ReadAll(r.Body)
			b = bytes.Replace(b, []byte("</body>"), []byte(`<script src="/__steps.js"></script></body>`), 1)
			r.Body = io.NopCloser(bytes.NewReader(b))
			r.Header.Set("Content-Length", strconv.Itoa(len(b)))
			r.ContentLength = int64(len(b))
		}
		return nil
	}
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/__steps.js" {
			w.Header().Set("Content-Type", "text/javascript")
			_, _ = w.Write([]byte(fmSteps))
			return
		}
		r.Header.Set("Cookie", e.cookie)
		r.Host = target.Host
		if r.Header.Get("Origin") != "" {
			r.Header.Set("Origin", e.url) // the panel compares Origin with Host, and the proxy has changed the Host
		}
		px.ServeHTTP(w, r)
	}))
	defer front.Close()
	cmd := exec.Command(browser, "--headless=new", "--disable-gpu", "--no-first-run", "--user-data-dir="+t.TempDir(), "--window-size=1280,900",
		"--virtual-time-budget=15000", "--dump-dom", front.URL+"/sites/blog/files?path=htdocs")
	done := make(chan []byte, 1)
	go func() { b, _ := cmd.Output(); done <- b }()
	var dom string
	select {
	case b := <-done:
		dom = string(b)
	case <-time.After(60 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("browser timed out")
	}
	i := strings.Index(dom, `<pre id="t-result">`)
	if i < 0 {
		t.Fatalf("the steps did not finish; page starts: %.400s", dom)
	}
	res := dom[i+len(`<pre id="t-result">`):]
	res = res[:strings.Index(res, "</pre>")]
	res = strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`).Replace(res)
	t.Log("\n" + res)
	if strings.Contains(res, "FAIL") {
		t.Error("some steps failed (see the log above)")
	}
	if strings.Count(res, "ok ") < 25 {
		t.Errorf("only %d steps passed", strings.Count(res, "ok "))
	}
	if b, err := os.ReadFile(filepath.Join(e.home, "htdocs", "up.txt")); err != nil || string(b) != "hello upload" {
		t.Errorf("the uploaded file: %q, %v", b, err)
	}
}
