package txn

import (
	"testing"
	"time"
)

func TestStart16ReplacesActiveOnSameConnector(t *testing.T) {
	s := New()
	a := s.Start16("CP1", 1, "A", 0, time.Time{})
	b := s.Start16("CP1", 1, "B", 10, time.Time{})
	c := s.Start16("CP2", 1, "C", 0, time.Time{})
	if a.ID != "1" || b.ID != "2" || c.ID != "3" {
		t.Fatalf("ids %s %s %s", a.ID, b.ID, c.ID)
	}
	old, _ := s.Get("CP1", "1")
	if old.Active || old.StoppedReason != "Replaced" {
		t.Fatalf("replaced = %+v", old)
	}
	if got := s.List(Filter{CPID: "CP1", ActiveOnly: true}); len(got) != 1 || got[0].ID != "2" {
		t.Fatalf("active CP1 = %+v", got)
	}
	if _, ok := s.Stop16("CP1", 1, 5, time.Time{}, ""); ok {
		t.Fatal("stopping an ended transaction succeeded")
	}
	if ended := s.EndAll("CP2", "Reboot"); len(ended) != 1 || ended[0].StoppedReason != "Reboot" {
		t.Fatalf("EndAll = %+v", ended)
	}
	if got := s.List(Filter{}); len(got) != 3 || got[0].Seq != 1 || got[2].Seq != 3 {
		t.Fatalf("List = %+v", got)
	}
}

func TestEndedListIsBounded(t *testing.T) {
	s := New()
	for range MaxEnded + 5 {
		tx := s.Start16("CP1", 1, "A", 0, time.Time{})
		var id int64
		for _, ch := range tx.ID {
			id = id*10 + int64(ch-'0')
		}
		s.Stop16("CP1", id, 1, time.Time{}, "")
	}
	if got := s.List(Filter{}); len(got) != MaxEnded || got[0].ID != "6" {
		t.Fatalf("kept %d, first %s", len(got), got[0].ID)
	}
}
