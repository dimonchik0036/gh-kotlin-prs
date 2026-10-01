package model

import (
	"testing"
	"time"
)

func TestAgo(t *testing.T) {
	now := time.Date(2026, 10, 1, 13, 0, 0, 0, time.UTC)
	for d, want := range map[time.Duration]string{
		30 * time.Second: "just now",
		5 * time.Minute:  "5m ago",
		2 * time.Hour:    "2h ago",
		47 * time.Hour:   "47h ago",
		72 * time.Hour:   "3d ago",
	} {
		if got := Ago(now, now.Add(-d)); got != want {
			t.Errorf("Ago(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestCodeOwnersMissing(t *testing.T) {
	s := CodeOwnersStatus{Rules: []CodeOwnerRule{
		{Paths: []string{"/a/"}, Mark: MarkApproved},
		{Paths: []string{"/b/"}, Mark: MarkReRequest},
		{Paths: []string{"/c/"}, Mark: MarkNoReview},
	}}
	if got := s.Missing(); len(got) != 2 || got[0].Paths[0] != "/b/" || got[1].Paths[0] != "/c/" {
		t.Errorf("Missing() = %+v", got)
	}
}
