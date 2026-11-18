package store

import "testing"

func TestTheWebMailChoiceOfASiteIsStored(t *testing.T) {
	s, path := open(t)
	if err := s.AddSite(phpSite("blog", "blog.example.com")); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetSite("blog")
	if err != nil || got.Webmail {
		t.Fatalf("a new site has no web mail: %+v %v", got, err)
	}
	got.Webmail = true
	if err := s.SetDomain(got); err != nil {
		t.Fatal(err)
	}
	again, _ := s.GetSite("blog")
	if !again.Webmail {
		t.Error("the choice must be stored")
	}
	s.Close()

	// and it survives opening the database again
	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if back, _ := s2.GetSite("blog"); !back.Webmail {
		t.Error("the choice must survive a restart")
	}
	got.Webmail = false
	if err := s2.SetDomain(got); err != nil {
		t.Fatal(err)
	}
	if off, _ := s2.GetSite("blog"); off.Webmail {
		t.Error("turning it off must be stored too")
	}
}
