package data

import (
	"io"
	"os"
	"unicode/utf8"

	"github.com/wangsrGit119/codingfire/internal/core"
)

// jsonlChunkSize is the read block size.
const jsonlChunkSize = 256 * 1024

// maxLineBytes guards against a "line" that is not really a line. Anything
// larger than this is not our format, so it is discarded rather than allowed to
// eat memory.
const maxLineBytes = 8 * 1024 * 1024

// LineParser turns one JSONL line into an event. Returning ok=false skips it.
type LineParser func(line, path string) (core.UsageEvent, bool)

// ReadNewJSONL incrementally reads a JSONL file, seeking to the stored byte
// cursor and reading only what is new. A trailing line split across a chunk
// boundary is carried to the next call.
//
// Semantics match the macOS version's readJSONLDetached, including two
// self-healing branches:
//   - the file was truncated (cursor beyond EOF) → re-read from the start
//   - the cursor is stuck at EOF but the file never produced an event → fall
//     back to 0, which fixes the old "always 0 tokens" bug
//
// Unlike the Swift version, line buffering is done on bytes rather than
// strings, so a multi-byte UTF-8 character split across a chunk boundary cannot
// turn into mojibake.
func ReadNewJSONL(path string, store *UsageStore, fromStart bool, parse LineParser) []core.UsageEvent {
	var events []core.UsageEvent
	if parse == nil {
		return events
	}

	cursorOffset, cursorPartial, hasPartial := store.FileCursor(path)

	fi, err := os.Stat(path)
	if err != nil {
		return events
	}
	size := fi.Size()

	truncated := cursorOffset > size
	stuckAtEOF := !fromStart && size > 0 && cursorOffset >= size && !store.HasEventsForFilePath(path)
	// Preserve the stored partial line until more bytes arrive. Already-read
	// logs need neither an open handle nor two 256 KiB scratch buffers.
	if !fromStart && !truncated && !stuckAtEOF && cursorOffset == size {
		return events
	}

	offset := cursorOffset
	var carry []byte
	switch {
	case fromStart || truncated || stuckAtEOF:
		offset = 0
	case hasPartial && cursorPartial != "":
		carry = DecodePartial(cursorPartial)
	}

	f, err := os.Open(path)
	if err != nil {
		return events
	}
	defer f.Close()

	if st, err := f.Stat(); err == nil && offset > st.Size() {
		offset = 0
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return events
	}

	buf := make([]byte, 0, jsonlChunkSize)
	buf = append(buf, carry...)
	chunk := make([]byte, jsonlChunkSize)

	for {
		n, err := f.Read(chunk)
		if n > 0 {
			buf = append(buf, chunk[:n]...)

			start := 0
			for i := 0; i < len(buf); i++ {
				if buf[i] != '\n' {
					continue
				}
				length := i - start
				// Tolerate CRLF.
				if length > 0 && buf[i-1] == '\r' {
					length--
				}
				if length > 0 {
					line := string(buf[start : start+length])
					// A byte slice cut at a chunk boundary can hold an
					// incomplete rune; skip rather than emit a replacement
					// character, because the parser would reject it anyway.
					if utf8.ValidString(line) {
						if ev, ok := parse(line, path); ok {
							events = append(events, ev)
						}
					}
				}
				start = i + 1
			}
			if start > 0 {
				buf = append(buf[:0], buf[start:]...)
			}
			if len(buf) > maxLineBytes {
				buf = buf[:0]
			}
		}
		if err != nil {
			break
		}
	}

	newOffset := offset
	if pos, err := f.Seek(0, io.SeekCurrent); err == nil {
		newOffset = pos
	}

	store.SetFileCursor(path, newOffset, EncodePartial(buf), len(buf) > 0)
	return events
}
