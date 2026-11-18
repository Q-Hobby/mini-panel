package dbadmin

import (
	"errors"
	"strings"
	"testing"
)

type fake struct {
	sqls    []string
	inUse   string // answer to the existence query
	failOn  string // fail any statement containing this text, with an error that quotes the SQL
	failErr bool
}

func (f *fake) Run(sql string) (string, error) {
	f.sqls = append(f.sqls, sql)
	if strings.Contains(sql, "information_schema") {
		if f.inUse == "" {
			return "0\n", nil
		}
		return f.inUse + "\n", nil
	}
	if f.failOn != "" && strings.Contains(sql, f.failOn) {
		return "", errors.New("syntax error near '" + sql + "'")
	}
	return "", nil
}

func (f *fake) last() string { return f.sqls[len(f.sqls)-1] }

func TestNames(t *testing.T) {
	for _, ok := range []string{"blog", "my_blog", "a", "9lives", strings.Repeat("a", 32)} {
		if !ValidName(ok) {
			t.Errorf("%q should be valid", ok)
		}
	}
	for _, bad := range []string{"", "Blog", "my-blog", "a b", "a`b", "a'b", "a;b", "x; DROP DATABASE mysql; --", strings.Repeat("a", 33), "_x"} {
		if ValidName(bad) {
			t.Errorf("%q should be refused", bad)
		}
	}
	if BaseName("my-blog") != "my_blog" {
		t.Error("hyphens become underscores")
	}
	for name, want := range map[string]bool{"my_blog": true, "my_blog_shop": true, "my_blogger": false, "other": false, "my": false} {
		if got := OwnedBy(name, "my-blog"); got != want {
			t.Errorf("OwnedBy(%q) = %v want %v", name, got, want)
		}
	}
}

func TestPasswords(t *testing.T) {
	a, err := NewPassword()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := NewPassword()
	if len(a) != 24 || a == b {
		t.Errorf("want two different 24 character passwords, got %q %q", a, b)
	}
	for _, c := range a {
		if !strings.ContainsRune(passwordAlphabet, c) {
			t.Errorf("character %q is outside the alphabet", c)
		}
	}
	if strings.ContainsAny(passwordAlphabet, "0O1lI") {
		t.Error("look-alike characters must stay out")
	}
	if !ValidPassword(a) {
		t.Error("a generated password must be accepted")
	}
	for _, bad := range []string{"", "short", "has a space1234", "quote'quote1234", `back\slash1234`, "tick`tick12345", "dq\"dq123456789", strings.Repeat("a", 65), "tab\there12345", "ünïcödé123456"} {
		if ValidPassword(bad) {
			t.Errorf("%q should be refused", bad)
		}
	}
	if !ValidPassword("Chosen-by_me.2026!#") {
		t.Error("ordinary punctuation is fine")
	}
}

func TestGrantEscapesUnderscores(t *testing.T) {
	s := createSQL("my_blog", "Pw123456789x")
	if !strings.Contains(s, "CREATE DATABASE `my_blog` ") {
		t.Errorf("the database name itself must not be escaped:\n%s", s)
	}
	if !strings.Contains(s, "GRANT ALL PRIVILEGES ON `my\\_blog`.* TO 'my_blog'@'localhost';") {
		t.Errorf("GRANT must escape _ or it matches other databases:\n%s", s)
	}
	if strings.Contains(s, "GRANT OPTION") || strings.Contains(s, "*.*") {
		t.Errorf("the user must get nothing beyond its own database:\n%s", s)
	}
}

func TestCreate(t *testing.T) {
	f := &fake{}
	if err := (Admin{f}).Create("my_blog", "Pw123456789x"); err != nil {
		t.Fatal(err)
	}
	if len(f.sqls) != 2 || !strings.Contains(f.last(), "CREATE USER 'my_blog'@'localhost' IDENTIFIED BY 'Pw123456789x'") {
		t.Errorf("sqls=%q", f.sqls)
	}
}

func TestCreateRefusesBadInputBeforeTouchingTheServer(t *testing.T) {
	f := &fake{}
	a := Admin{f}
	if err := a.Create("x`; DROP DATABASE mysql; --", "Pw123456789x"); err == nil {
		t.Error("an unsafe name must be refused")
	}
	if err := a.Create("blog", "it's-a-password-1"); err == nil {
		t.Error("a password with a quote must be refused")
	}
	if len(f.sqls) != 0 {
		t.Errorf("nothing may be sent to the server: %q", f.sqls)
	}
}

func TestCreateNeverTakesOverAnExistingDatabase(t *testing.T) {
	f := &fake{inUse: "1"}
	err := Admin{f}.Create("mysql", "Pw123456789x")
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("err=%v", err)
	}
	if len(f.sqls) != 1 {
		t.Errorf("only the existence check may run: %q", f.sqls)
	}
}

func TestCreateCleansUpAndHidesThePasswordOnFailure(t *testing.T) {
	f := &fake{failOn: "GRANT"}
	err := Admin{f}.Create("blog", "SecretPw123456")
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "SecretPw123456") || !strings.Contains(err.Error(), "********") {
		t.Errorf("the password leaked into the error: %v", err)
	}
	if !strings.Contains(f.last(), "DROP DATABASE IF EXISTS `blog`") {
		t.Errorf("a half-made database must be removed, last sql: %q", f.last())
	}
}

func TestSetPassword(t *testing.T) {
	f := &fake{inUse: "2"}
	if err := (Admin{f}).SetPassword("blog", "NewPassword12345"); err != nil {
		t.Fatal(err)
	}
	if f.last() != "ALTER USER 'blog'@'localhost' IDENTIFIED BY 'NewPassword12345';\n" {
		t.Errorf("sql=%q", f.last())
	}
	gone := &fake{}
	if err := (Admin{gone}).SetPassword("nothere", "NewPassword12345"); err == nil || len(gone.sqls) != 1 {
		t.Errorf("a missing user must be reported without an ALTER, sqls=%q err=%v", gone.sqls, err)
	}
	if err := (Admin{&fake{inUse: "2"}}).SetPassword("blog", "weak"); err == nil {
		t.Error("a weak password must be refused")
	}
}

func TestDrop(t *testing.T) {
	f := &fake{}
	if err := (Admin{f}).Drop("my_blog"); err != nil {
		t.Fatal(err)
	}
	if f.last() != "DROP DATABASE IF EXISTS `my_blog`;\nDROP USER IF EXISTS 'my_blog'@'localhost';\n" {
		t.Errorf("sql=%q", f.last())
	}
	if err := (Admin{f}).Drop("a;b"); err == nil {
		t.Error("an unsafe name must be refused")
	}
}
