package data

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/wangsrGit119/codingfire/internal/core"
)

// debounceWindow coalesces the burst of events one write produces into a
// single scan.
const debounceWindow = 200 * time.Millisecond

// LogWatcher makes scanning driven by "a log file was actually written" rather
// than by waiting out the 4s poll. Time from a tool writing a log line to the
// fire reacting drops from a worst-case full scan period to about 200ms.
//
// Three rules keep it safe:
//   - Debounce: one write emits several events (Create + Change×N). They
//     collapse into one scan.
//   - Overflow: a full internal buffer raises Error, and by then events are
//     already lost, so we must degrade to a full scan. Never stay silent —
//     missing a read costs far more than an extra scan (the incremental
//     cursors guarantee a rescan cannot double-count).
//   - Degrade: any failure just means one fewer watch point. The heartbeat
//     scan still covers it, and the next heartbeat retries the watch.
type LogWatcher struct {
	onChanged  func()
	onOverflow func()

	mu       sync.Mutex
	watcher  *fsnotify.Watcher
	watched  map[string]struct{}
	watcherN int
	timer    *time.Timer
	disposed bool

	// OverflowCount is how many times the OS watch buffer overflowed.
	OverflowCount int

	doneCh chan struct{}
	once   sync.Once
}

// NewLogWatcher starts a watcher. onOverflow may be nil.
func NewLogWatcher(onChanged, onOverflow func()) *LogWatcher {
	w := &LogWatcher{
		onChanged:  onChanged,
		onOverflow: onOverflow,
		watched:    map[string]struct{}{},
		doneCh:     make(chan struct{}),
	}
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		// Without a watcher the heartbeat scan is the only mechanism left.
		core.LogWarn("log watcher unavailable: " + err.Error())
		close(w.doneCh)
		return w
	}
	w.watcher = fsw
	go w.loop()
	return w
}

func (w *LogWatcher) loop() {
	for {
		select {
		case <-w.doneCh:
			return
		case _, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			w.notify()
		case _, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			w.onError()
		}
	}
}

// WatchedRoots is how many roots have a live watch. Zero means watching never
// came up and only the heartbeat scan is running.
func (w *LogWatcher) WatchedRoots() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.watcherN
}

// Sync attaches watches for these roots. Roots already watched are skipped;
// roots that do not exist yet are retried on the next call, because a tool
// installed later only needs watching from the moment it appears.
func (w *LogWatcher) Sync(roots []string) {
	w.mu.Lock()
	disposed := w.disposed
	w.mu.Unlock()
	if disposed || w.watcher == nil || len(roots) == 0 {
		return
	}

	for _, root := range roots {
		if root == "" {
			continue
		}
		key, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		key = strings.TrimRight(strings.TrimRight(key, `\`), "/")

		w.mu.Lock()
		if _, seen := w.watched[key]; seen {
			w.mu.Unlock()
			continue
		}
		w.watched[key] = struct{}{}
		w.mu.Unlock()

		if err := w.tryWatch(root); err != nil {
			// Not present yet, or no permission — allow the next heartbeat to
			// retry by forgetting the key.
			w.mu.Lock()
			delete(w.watched, key)
			w.mu.Unlock()
			continue
		}
		w.mu.Lock()
		w.watcherN++
		w.mu.Unlock()
	}
}

func (w *LogWatcher) tryWatch(root string) error {
	dir := root
	if fi, err := os.Stat(root); err == nil && !fi.IsDir() {
		// The root is a single file (Qoder's local.db and friends). Watch its
		// directory rather than the exact name: SQLite's -wal and -shm files
		// count as changes too.
		dir = filepath.Dir(root)
	} else if err != nil {
		return err
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return os.ErrNotExist
	}
	// On Windows fsnotify maps this to ReadDirectoryChangesW with
	// bWatchSubtree, so the subtree is covered without walking it.
	return w.watcher.Add(dir)
}

// notify schedules a scan 200ms from the first un-coalesced event.
//
// Deliberately not a resetting debounce: if every event pushed the deadline
// back, a log appended to more often than every 200ms would postpone the
// callback forever — least responsive exactly when it is busiest. Firing 200ms
// after the first event merges everything that arrives in between.
func (w *LogWatcher) notify() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.disposed || w.timer != nil {
		return
	}
	w.timer = time.AfterFunc(debounceWindow, w.fire)
}

func (w *LogWatcher) fire() {
	w.mu.Lock()
	w.timer = nil
	disposed := w.disposed
	cb := w.onChanged
	w.mu.Unlock()

	if disposed || cb == nil {
		return
	}
	// A panic in the callback must not kill the watcher goroutine.
	defer func() { _ = recover() }()
	cb()
}

func (w *LogWatcher) onError() {
	w.mu.Lock()
	disposed := w.disposed
	w.mu.Unlock()
	if disposed {
		return
	}
	w.mu.Lock()
	w.OverflowCount++
	cb := w.onOverflow
	w.mu.Unlock()

	if cb == nil {
		return
	}
	defer func() { _ = recover() }()
	cb()
}

// Close stops watching and releases handles.
func (w *LogWatcher) Close() {
	w.once.Do(func() {
		w.mu.Lock()
		w.disposed = true
		if w.timer != nil {
			w.timer.Stop()
			w.timer = nil
		}
		fsw := w.watcher
		w.watcher = nil
		w.mu.Unlock()

		close(w.doneCh)
		if fsw != nil {
			_ = fsw.Close()
		}
	})
}
