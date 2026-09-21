package data

import (
	"bytes"
	"os"
	"strings"
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

// TestModelSurvivesRoundTrip pins the on-disk spelling of the model field, and
// the two cases that make it awkward: a model name with quotes and backslashes,
// and no model at all.
func TestModelSurvivesRoundTrip(t *testing.T) {
	cases := []string{
		"deepseek-v4.1-flash",
		`weird "quoted\name"`,
		"",
	}
	for _, model := range cases {
		in := core.UsageEvent{
			ID:        "e1",
			Source:    core.UsageSourcesAll[0],
			Timestamp: time.Now(),
			Tokens:    7,
			Model:     model,
		}
		out, ok := decodeLine(encodeLine(in))
		if !ok {
			t.Fatalf("model %q: line did not decode", model)
		}
		if out.Model != model {
			t.Errorf("model %q came back as %q (line %s)", model, out.Model, encodeLine(in))
		}
		if model == "" && strings.Contains(encodeLine(in), `"m"`) {
			t.Errorf("an empty model should not be written at all: %s", encodeLine(in))
		}
	}
}

// TestTodayByModelOnlyCountsStatedModels: the per-model split is a partial view
// by nature, so it must not quietly attribute an unstated model to anything.
func TestTodayByModelOnlyCountsStatedModels(t *testing.T) {
	t.Setenv("CODINGFIRE_DATA_DIR", t.TempDir())
	now := time.Now()
	stored := []core.UsageEvent{
		{ID: "a", Source: core.UsageSourcesAll[0], Timestamp: now, Tokens: 10, Model: "m-one"},
		{ID: "b", Source: core.UsageSourcesAll[0], Timestamp: now, Tokens: 20, Model: "m-one"},
		{ID: "c", Source: core.UsageSourcesAll[0], Timestamp: now, Tokens: 30, Model: "m-two"},
		{ID: "d", Source: core.UsageSourcesAll[0], Timestamp: now, Tokens: 40},
	}
	var body bytes.Buffer
	for _, e := range stored {
		body.WriteString(encodeLine(e) + "\n")
	}
	if err := os.WriteFile(core.AppPaths.UsageFile(), body.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}

	s := NewUsageStore()
	s.Open()
	defer s.Close()

	totals := s.TodayTotals(now)
	if totals.Total != 100 {
		t.Fatalf("total = %d, want 100", totals.Total)
	}
	if got := totals.ByModel["m-one"]; got != 30 {
		t.Errorf("m-one = %d, want 30", got)
	}
	if got := totals.ByModel["m-two"]; got != 30 {
		t.Errorf("m-two = %d, want 30", got)
	}
	if len(totals.ByModel) != 2 {
		t.Errorf("by-model should hold exactly the stated models, got %v", totals.ByModel)
	}
	if sum := totals.ByModel["m-one"] + totals.ByModel["m-two"]; sum >= totals.Total {
		t.Errorf("the model split sums to %d of %d; the unstated event must stay out", sum, totals.Total)
	}
}
