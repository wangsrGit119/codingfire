package data

import (
	"bytes"
	"os"
	"testing"
	"time"

	"github.com/wangsrGit119/codingfire/internal/core"
)

func TestStreamHistoryRetention(t *testing.T) {
	t.Setenv("CODINGFIRE_DATA_DIR", t.TempDir())
	now := time.Now()
	recent := core.UsageEvent{ID: "recent", Tokens: 42, Timestamp: now, Source: core.UsageSourcesAll[0]}
	old := recent
	old.ID, old.Timestamp = "old", now.AddDate(0, 0, -retainDays-1)
	body := encodeLine(old) + "\ninvalid\n" + encodeLine(recent) + "\n"
	if err := os.WriteFile(core.AppPaths.UsageFile(), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	s := NewUsageStore()
	s.Open()
	defer s.Close()
	if len(s.events) != 1 || s.TodayTotals(now).Total != 42 {
		t.Fatalf("loaded events: %v", s.events)
	}
	got, err := os.ReadFile(core.AppPaths.UsageFile())
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != encodeLine(recent)+"\n" {
		t.Fatal("history was not compacted correctly")
	}
}

func TestIncompleteHistoryIsNotCompacted(t *testing.T) {
	t.Setenv("CODINGFIRE_DATA_DIR", t.TempDir())
	body := append([]byte("invalid\n"), bytes.Repeat([]byte("x"), 8*1024*1024+1)...)
	path := core.AppPaths.UsageFile()
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	s := NewUsageStore()
	s.Open()
	defer s.Close()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Fatal("partial read truncated the original history")
	}
}
