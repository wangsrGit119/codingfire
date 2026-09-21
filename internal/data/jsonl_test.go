package data

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wangsrGit119/codingfire/internal/core"
)

func BenchmarkReadUnchangedJSONL(b *testing.B) {
	path := filepath.Join(b.TempDir(), "usage.jsonl")
	if err := os.WriteFile(path, []byte("usage\n"), 0600); err != nil {
		b.Fatal(err)
	}
	store := NewUsageStore()
	store.InsertEvent(core.UsageEvent{ID: "usage", FilePath: path, Timestamp: time.Now(), Tokens: 1})
	store.SetFileCursor(path, 6, "", false)
	parse := func(string, string) (core.UsageEvent, bool) { return core.UsageEvent{}, false }
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ReadNewJSONL(path, store, false, parse)
	}
}

func TestJSONLIdleAppendPartialAndTruncate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	write := func(text string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	store := NewUsageStore()
	calls := 0
	parse := func(line, path string) (core.UsageEvent, bool) {
		calls++
		return core.UsageEvent{ID: line, FilePath: path, Timestamp: time.Now(), Tokens: 1}, true
	}
	write("first\npar")
	events := ReadNewJSONL(path, store, false, parse)
	if len(events) != 1 {
		t.Fatalf("first read: %v", events)
	}
	store.InsertEvent(events[0])
	if got := ReadNewJSONL(path, store, false, parse); len(got) != 0 || calls != 1 {
		t.Fatal("idle read reparsed data")
	}
	_, partial, hasPartial := store.FileCursor(path)
	if !hasPartial || string(DecodePartial(partial)) != "par" {
		t.Fatal("idle read lost partial line")
	}
	write("first\npartial\n")
	events = ReadNewJSONL(path, store, false, parse)
	if len(events) != 1 || events[0].ID != "partial" {
		t.Fatalf("append: %v", events)
	}
	write("new\n")
	events = ReadNewJSONL(path, store, false, parse)
	if len(events) != 1 || events[0].ID != "new" {
		t.Fatalf("truncate: %v", events)
	}
	if got := ReadNewJSONL(path, store, true, parse); len(got) != 1 {
		t.Fatal("explicit rescan skipped")
	}
}

func TestJSONLRecoversCursorWithoutEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	if err := os.WriteFile(path, []byte("usage\n"), 0600); err != nil {
		t.Fatal(err)
	}
	store := NewUsageStore()
	store.SetFileCursor(path, 6, "", false)
	events := ReadNewJSONL(path, store, false, func(line, path string) (core.UsageEvent, bool) {
		return core.UsageEvent{ID: line, FilePath: path, Tokens: 1}, true
	})
	if len(events) != 1 {
		t.Fatal("EOF recovery skipped")
	}
}
