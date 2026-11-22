package store

import (
	"errors"
	"testing"
	"time"
)

// A pairing code works once and not after its time; pairing a name again gives the machine a new key, not a second row.
func TestNotifyPairingWorksOnce(t *testing.T) {
	s, _ := open(t)
	if err := s.AddNotifyPair("h1", "web-2", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	n, err := s.UseNotifyPair("h1", "k1")
	if err != nil || n.Name != "web-2" || n.ID == 0 {
		t.Fatalf("%+v %v", n, err)
	}
	if _, err := s.UseNotifyPair("h1", "k2"); !errors.Is(err, ErrNotFound) {
		t.Errorf("used twice: %v", err)
	}
	_ = s.AddNotifyPair("old", "web-3", time.Now().Add(-time.Minute))
	if _, err := s.UseNotifyPair("old", "k"); !errors.Is(err, ErrNotFound) {
		t.Errorf("an expired code worked: %v", err)
	}
	_ = s.AddNotifyPair("h2", "web-2", time.Now().Add(time.Hour))
	again, err := s.UseNotifyPair("h2", "k3")
	nodes, _ := s.NotifyNodes()
	if err != nil || again.ID != n.ID || len(nodes) != 1 || nodes[0].KeyHash != "k3" {
		t.Fatalf("%+v %+v %v", again, nodes, err)
	}
}

func TestNotifyNodeLostAndBack(t *testing.T) {
	s, _ := open(t)
	_ = s.AddNotifyPair("h", "db-1", time.Now().Add(time.Hour))
	n, _ := s.UseNotifyPair("h", "k")
	if was, err := s.SeeNotifyNode(n.ID, `{"version":"1.3.0"}`); err != nil || was {
		t.Fatalf("%v %v", was, err)
	}
	_ = s.SetNotifyNodeLost(n.ID, true)
	if was, _ := s.SeeNotifyNode(n.ID, `{}`); !was {
		t.Error("the hub would not say the machine is back")
	}
	if got, _ := s.NotifyNode(n.ID); got.Lost || got.Report != `{}` {
		t.Errorf("%+v", got)
	}
	if err := s.SetNotifyNodeRules(999, ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("%v", err)
	}
	if err := s.DeleteNotifyNode(n.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.NotifyNode(n.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("%v", err)
	}
}

func TestNotifyOutboxKeepsOrder(t *testing.T) {
	s, _ := open(t)
	for _, m := range []string{"a", "b", "c"} {
		_ = s.QueueNotify(m)
	}
	msgs, _ := s.NotifyOutbox(2)
	if len(msgs) != 2 || msgs[0].Text != "a" || msgs[1].Text != "b" {
		t.Fatalf("%+v", msgs)
	}
	_ = s.DoneNotify(msgs[0].ID)
	msgs, _ = s.NotifyOutbox(10)
	if len(msgs) != 2 || msgs[0].Text != "b" {
		t.Fatalf("%+v", msgs)
	}
	_ = s.AddNotifyChat(-100, " Ops ")
	_ = s.AddNotifyChat(-100, "Ops team")
	chats, _ := s.NotifyChats()
	if len(chats) != 1 || chats[0].Title != "Ops team" {
		t.Errorf("%+v", chats)
	}
}
