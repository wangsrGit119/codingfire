//
//  sqlite.go — CodingFire for Go
//
//  Minimal read-only SQLite reader, a port of SqliteReader.cs.
//
//  Motivation: several AI coding tools (ZCode / OpenCode / Qoder / Goose /
//  Zed / Warp…) keep their usage in a SQLite file. CodingFire ships as a
//  single dependency-free binary, so it cannot pull in a native sqlite3.dll.
//  This file therefore parses the file format by hand and never executes SQL:
//
//    - table b-tree pages only (leaf 0x0D / interior 0x05); index pages skipped
//    - overflow pages followed along the chain when a payload does not fit
//    - WAL: the committed prefix of a -wal file is overlaid on the main file
//    - no SQL: the schema is recovered from sqlite_master and columns are
//      selected by name
//
//  Not done: writing, transactions, indexes, checksum verification, page
//  cache eviction. Checksums are not verified, so the last uncommitted frame
//  of a WAL being written could in principle be misread; truncating the
//  overlay at the last commit frame avoids that in practice.
//
//  Everything read out of the file is treated as untrusted input: a corrupt or
//  half-written database must yield empty results, never a panic and never an
//  unbounded allocation.
//

package data

import (
	"encoding/binary"
	"math"
	"os"
	"strings"
)

// Hard limits. A malformed file must not be able to make us allocate without
// bound, recurse without end, or loop without end.
const (
	sqliteHeaderSize   = 100   // bytes of file header before page 1's b-tree
	sqliteMinPageSize  = 512   // SQLite's smallest legal page size
	sqliteMaxPageSize  = 65536 // largest legal page size (page size field == 1)
	sqliteMaxDepth     = 64    // b-tree recursion cap
	sqliteMaxPayload   = 64 << 20
	sqliteMaxSerial    = 4096 // serial types per record
	sqliteMaxRows      = 2_000_000
	sqliteMaxPages     = 1 << 20 // pages held in cache / visited in a walk
	sqliteMaxOverflow  = 200000  // overflow-page chain hops per cell
	sqliteMaxWalFrames = 1 << 20
	sqliteMaxWalBytes  = 64 << 20 // pending (uncommitted) WAL bytes held at once
)

// SqliteTable is one table's schema, mirroring the C# SqliteTable.
type SqliteTable struct {
	Name     string
	RootPage int
	Sql      string
	Columns  []string
}

// IndexOf returns the ordinal of column (case-insensitive) or -1.
func (t *SqliteTable) IndexOf(column string) int {
	if t == nil {
		return -1
	}
	for i, c := range t.Columns {
		if strings.EqualFold(c, column) {
			return i
		}
	}
	return -1
}

// SqliteDB is a read-only, hand-parsed SQLite database handle. It corresponds
// to the C# SqliteDb: Open → HasTable/Table → Rows → Close.
//
// A SqliteDB is not safe for concurrent use; each adapter opens its own.
type SqliteDB struct {
	f         *os.File
	size      int64
	pageSize  int
	reserved  int
	pageCount int

	pages  map[int][]byte
	wal    map[int][]byte
	tables map[string]*SqliteTable
	closed bool
}

// OpenSqlite opens path read-only and parses its header, WAL overlay and
// schema. It returns nil when the file is missing, unreadable or not a SQLite
// database. The file is opened with share-all semantics so a tool that has the
// database open (possibly in WAL mode, possibly mid-write) is not disturbed.
//
// This is the Go spelling of the C# SqliteDb.Open.
func OpenSqlite(path string) *SqliteDB {
	if path == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	fi, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil
	}

	db := &SqliteDB{
		f:        f,
		size:     fi.Size(),
		pageSize: 4096,
		pages:    make(map[int][]byte),
		wal:      make(map[int][]byte),
		tables:   make(map[string]*SqliteTable),
	}
	if !db.readHeader() {
		db.Close()
		return nil
	}
	if db.size > 0 {
		db.pageCount = int(db.size / int64(db.pageSize))
	}
	db.readWal(path)
	db.loadSchema()
	return db
}

// Close releases the file handle and cached pages. It is safe on nil.
func (db *SqliteDB) Close() {
	if db == nil || db.closed {
		return
	}
	db.closed = true
	if db.f != nil {
		_ = db.f.Close()
		db.f = nil
	}
	db.pages = nil
	db.wal = nil
}

// HasTable reports whether the database defines a table with this name.
func (db *SqliteDB) HasTable(name string) bool {
	return db.Table(name) != nil
}

// Table returns the schema of name, or nil when there is no such table.
func (db *SqliteDB) Table(name string) *SqliteTable {
	if db == nil || db.tables == nil {
		return nil
	}
	return db.tables[strings.ToLower(name)]
}

// ---------------------------------------------------------------------------
// Header
// ---------------------------------------------------------------------------

func (db *SqliteDB) readHeader() bool {
	h := make([]byte, sqliteHeaderSize)
	if !db.readAt(h, 0) {
		return false
	}
	const magic = "SQLite format 3"
	if string(h[:len(magic)]) != magic {
		return false
	}

	ps := be16(h, 16)
	if ps == 1 {
		// The page-size field cannot express 65536, so 1 means 65536.
		db.pageSize = 65536
	} else {
		db.pageSize = ps
	}
	if db.pageSize < sqliteMinPageSize || db.pageSize > sqliteMaxPageSize {
		return false
	}
	if db.pageSize&(db.pageSize-1) != 0 { // must be a power of two
		return false
	}

	db.reserved = int(h[20])
	if db.reserved >= db.pageSize {
		return false
	}
	return true
}

// readAt fills buf from offset, reporting whether it got every byte. A short
// read (file truncated or being rewritten) is a failure, not an error to
// propagate: callers fall back to "no data".
func (db *SqliteDB) readAt(buf []byte, off int64) bool {
	if db.f == nil || off < 0 {
		return false
	}
	n, err := db.f.ReadAt(buf, off)
	if n != len(buf) {
		return false
	}
	_ = err
	return true
}

// ---------------------------------------------------------------------------
// WAL
// ---------------------------------------------------------------------------

type walFrame struct {
	pageNo int
	page   []byte
}

// readWal overlays the committed pages of path+"-wal" onto the database. A
// missing WAL is normal. Frames are applied only up to the last frame whose
// commit-size field is non-zero: anything after that belongs to a transaction
// that has not committed and is deliberately dropped.
func (db *SqliteDB) readWal(dbPath string) {
	f, err := os.Open(dbPath + "-wal")
	if err != nil {
		return // no WAL, or it is locked away — nothing to overlay
	}
	defer func() { _ = f.Close() }()

	fi, err := f.Stat()
	if err != nil {
		return
	}
	length := fi.Size()
	if length < 32 {
		return
	}

	hdr := make([]byte, 32)
	if n, _ := f.ReadAt(hdr, 0); n != 32 {
		return
	}
	magic := be32(hdr, 0)
	if magic != 0x377f0682 && magic != 0x377f0683 {
		return
	}

	walPageSize := int(be32(hdr, 8))
	if walPageSize < sqliteMinPageSize || walPageSize > sqliteMaxPageSize ||
		walPageSize&(walPageSize-1) != 0 {
		walPageSize = db.pageSize
	}
	salt1 := be32(hdr, 16)
	salt2 := be32(hdr, 20)

	frameSize := int64(24 + walPageSize)
	frameCount := (length - 32) / frameSize
	if frameCount <= 0 {
		return
	}
	if frameCount > sqliteMaxWalFrames {
		frameCount = sqliteMaxWalFrames
	}

	frameHdr := make([]byte, 24)
	pageBuf := make([]byte, walPageSize)

	var pending []walFrame
	pendingBytes := int64(0)

	for i := int64(0); i < frameCount; i++ {
		off := 32 + i*frameSize
		if n, _ := f.ReadAt(frameHdr, off); n != 24 {
			break
		}
		// A salt mismatch means these frames belong to an older WAL cycle.
		if be32(frameHdr, 8) != salt1 || be32(frameHdr, 12) != salt2 {
			break
		}
		pageNo := int(be32(frameHdr, 0))
		commitSize := be32(frameHdr, 4)
		if pageNo <= 0 {
			break
		}
		if n, _ := f.ReadAt(pageBuf, off+24); n != walPageSize {
			break
		}

		cp := make([]byte, walPageSize)
		copy(cp, pageBuf)
		pending = append(pending, walFrame{pageNo: pageNo, page: cp})
		pendingBytes += int64(walPageSize)

		if commitSize != 0 {
			for _, fr := range pending {
				if len(db.wal) >= sqliteMaxPages {
					return
				}
				db.wal[fr.pageNo] = fr.page
			}
			pending = pending[:0]
			pendingBytes = 0
			continue
		}
		// Uncommitted frames still accumulate, so cap how much they may hold.
		if pendingBytes > sqliteMaxWalBytes {
			break
		}
	}
	// Whatever is left in pending is uncommitted — intentionally discarded.
}

// ---------------------------------------------------------------------------
// Page access
// ---------------------------------------------------------------------------

// readPage returns one page, preferring the WAL overlay. Pages are checked
// against the file size only after the WAL, because an uncheckpointed WAL can
// legitimately describe pages beyond the end of the main file.
func (db *SqliteDB) readPage(pageNo int) []byte {
	if db == nil || pageNo <= 0 {
		return nil
	}
	if p, ok := db.wal[pageNo]; ok {
		return p
	}
	if p, ok := db.pages[pageNo]; ok {
		return p
	}
	if pageNo > db.pageCount {
		return nil
	}
	buf := make([]byte, db.pageSize)
	if !db.readAt(buf, int64(pageNo-1)*int64(db.pageSize)) {
		return nil
	}
	if len(db.pages) < sqliteMaxPages {
		db.pages[pageNo] = buf
	}
	return buf
}

// headerBase is where a page's b-tree header starts. Page 1 carries the
// 100-byte file header, so its b-tree header is at offset 100. Reading it at
// offset 0 yields the 'S' of "SQLite format 3" and silently hides every table
// in a small database whose sqlite_master root is page 1 itself.
func headerBase(pageNo int) int {
	if pageNo == 1 {
		return sqliteHeaderSize
	}
	return 0
}

// ---------------------------------------------------------------------------
// Schema
// ---------------------------------------------------------------------------

// loadSchema walks sqlite_master (root page 1) and records every table.
func (db *SqliteDB) loadSchema() {
	// sqlite_master rows are (type,name,tbl_name,rootpage,sql).
	for _, lp := range db.collectLeaves(1) {
		page := db.readPage(lp)
		if page == nil {
			continue
		}
		b := headerBase(lp)
		if b+8 > len(page) || page[b] != 0x0D {
			continue
		}
		n := be16(page, b+3)
		for i := 0; i < n; i++ {
			co := be16(page, b+8+i*2)
			rec := db.readLeafCell(page, co)
			if len(rec) < 5 {
				continue
			}
			kind, _ := rec[0].(string)
			if kind != "table" {
				continue
			}
			name, _ := rec[1].(string)
			if name == "" || strings.HasPrefix(name, "sqlite_") {
				continue
			}

			t := &SqliteTable{
				Name:     name,
				RootPage: toInt(rec[3]),
				Sql:      asString(rec[4]),
			}
			parseColumns(t)
			db.tables[strings.ToLower(name)] = t
		}
	}
}

// parseColumns pulls column names out of a CREATE TABLE statement. Only
// top-level comma-separated definitions count; table constraints
// (CONSTRAINT / PRIMARY / UNIQUE / CHECK / FOREIGN / KEY) are skipped.
func parseColumns(t *SqliteTable) {
	sql := t.Sql
	open := strings.IndexByte(sql, '(')
	if open < 0 {
		return
	}

	var sb []byte
	var defs []string
	depth := 0
	inStr := false
	var quote byte
	done := false

	for i := open + 1; i < len(sql) && !done; i++ {
		c := sql[i]
		if inStr {
			sb = append(sb, c)
			if c == quote {
				inStr = false
			}
			continue
		}
		switch c {
		case '\'', '"', '`':
			inStr = true
			quote = c
			sb = append(sb, c)
		case '(':
			depth++
			sb = append(sb, c)
		case ')':
			if depth == 0 {
				defs = append(defs, string(sb))
				done = true
				continue
			}
			depth--
			sb = append(sb, c)
		case ',':
			if depth == 0 {
				defs = append(defs, string(sb))
				sb = sb[:0]
				continue
			}
			sb = append(sb, c)
		default:
			sb = append(sb, c)
		}
	}

	for _, raw := range defs {
		def := strings.TrimSpace(raw)
		if def == "" {
			continue
		}
		head := firstToken(def)
		if head == "" {
			continue
		}
		switch strings.ToUpper(head) {
		case "CONSTRAINT", "PRIMARY", "UNIQUE", "CHECK", "FOREIGN", "KEY":
			continue
		}
		t.Columns = append(t.Columns, unquote(head))
	}
}

// firstToken returns the leading identifier of a column definition, keeping
// any quoting so that unquote can strip it later.
func firstToken(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if q := s[0]; q == '"' || q == '`' || q == '[' {
		closing := q
		if q == '[' {
			closing = ']'
		}
		e := strings.IndexByte(s[1:], closing)
		if e < 0 {
			return s[1:]
		}
		return s[:e+2]
	}
	sp := 0
	for sp < len(s) && !isSpace(s[sp]) && s[sp] != '(' {
		sp++
	}
	return s[:sp]
}

func isSpace(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\r', '\v', '\f':
		return true
	}
	return false
}

func unquote(s string) string {
	if len(s) >= 2 {
		f, l := s[0], s[len(s)-1]
		if (f == '"' && l == '"') || (f == '`' && l == '`') || (f == '[' && l == ']') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// ---------------------------------------------------------------------------
// b-tree walk
// ---------------------------------------------------------------------------

// collectLeaves returns the leaf pages reachable from pageNo, in order.
// Interior pages (0x05) are descended, anything else (index pages, free
// pages, corruption) is ignored. visited both prevents cycles and bounds the
// walk; depth bounds recursion.
func (db *SqliteDB) collectLeaves(pageNo int) []int {
	var out []int
	visited := make(map[int]bool)

	var walk func(p, depth int)
	walk = func(p, depth int) {
		if p <= 0 || depth > sqliteMaxDepth || visited[p] || len(visited) >= sqliteMaxPages {
			return
		}
		visited[p] = true

		page := db.readPage(p)
		if page == nil {
			return
		}
		b := headerBase(p)
		if b+8 > len(page) {
			return
		}

		switch page[b] {
		case 0x0D: // leaf table page
			if len(out) < sqliteMaxPages {
				out = append(out, p)
			}
			return
		case 0x05: // interior table page
		default:
			return
		}

		n := be16(page, b+3)
		ptr := b + 12
		for i := 0; i < n; i++ {
			if ptr+2 > len(page) {
				break
			}
			co := be16(page, ptr)
			ptr += 2
			if co+4 > len(page) {
				continue
			}
			walk(int(be32(page, co)), depth+1)
		}
		walk(int(be32(page, b+8)), depth+1) // right-most child
	}

	walk(pageNo, 0)
	return out
}

// readLeafCell decodes the table leaf cell at offset, following overflow pages
// when the payload does not fit locally. It returns nil for anything it cannot
// read in full — a truncated chain yields no row rather than half a row.
func (db *SqliteDB) readLeafCell(page []byte, offset int) []any {
	if offset <= 0 || offset >= len(page) {
		return nil
	}

	pos := offset
	payloadLen := readVarint(page, &pos)
	readVarint(page, &pos) // rowid — not needed

	if payloadLen < 0 || payloadLen > sqliteMaxPayload {
		return nil
	}

	u := db.pageSize - db.reserved
	if u < 4 {
		return nil
	}
	x := u - 35

	var local int64
	if payloadLen <= int64(x) {
		local = payloadLen
	} else {
		m := ((u - 12) * 32 / 255) - 23
		if m < 0 {
			return nil
		}
		k := int64(m) + (payloadLen-int64(m))%int64(u-4)
		if k <= int64(x) {
			local = k
		} else {
			local = int64(m)
		}
	}
	if local < 0 {
		local = 0
	}
	if local > payloadLen {
		local = payloadLen
	}

	body := make([]byte, payloadLen)
	copied := int(local)
	if pos+copied > len(page) {
		return nil
	}
	copy(body, page[pos:pos+copied])

	if payloadLen > local {
		next := 0
		if pos+int(local)+4 <= len(page) {
			next = int(be32(page, pos+int(local)))
		}
		remaining := payloadLen - local
		guard := 0
		for next > 0 && remaining > 0 && guard < sqliteMaxOverflow {
			guard++
			op := db.readPage(next)
			if op == nil {
				break
			}
			chunk := int64(u - 4)
			if chunk > remaining {
				chunk = remaining
			}
			if chunk <= 0 || len(op) <= 4 {
				break
			}
			if chunk > int64(len(op)-4) {
				chunk = int64(len(op) - 4)
			}
			dst := int(payloadLen - remaining)
			if dst < 0 || dst+int(chunk) > len(body) {
				break
			}
			copy(body[dst:dst+int(chunk)], op[4:4+int(chunk)])
			remaining -= chunk
			next = int(be32(op, 0))
		}
		if remaining > 0 {
			return nil // chain broken: no row beats half a row
		}
	}

	return decodeRecord(body)
}

// ---------------------------------------------------------------------------
// Record decoding
// ---------------------------------------------------------------------------

// decodeRecord decodes one record: a header of serial types followed by the
// values they describe.
func decodeRecord(b []byte) []any {
	pos := 0
	headerSize := readVarint(b, &pos)
	if headerSize <= 0 || headerSize > int64(len(b)) {
		return nil
	}
	headerEnd := int(headerSize)

	var serials []int64
	p := pos
	for p < headerEnd {
		serials = append(serials, readVarint(b, &p))
		if len(serials) > sqliteMaxSerial {
			break
		}
	}

	values := make([]any, len(serials))
	vp := headerEnd
	for i, st := range serials {
		var v any
		need := 0
		switch {
		case st == 0:
			v = nil
		case st >= 1 && st <= 6:
			need = []int{1, 2, 3, 4, 6, 8}[st-1]
			if vp+need <= len(b) {
				v = readBigEndianInt(b, vp, need)
			} else {
				need = 0
			}
		case st == 7:
			need = 8
			if vp+8 <= len(b) {
				v = math.Float64frombits(binary.BigEndian.Uint64(b[vp : vp+8]))
			} else {
				need = 0
			}
		case st == 8:
			v = int64(0)
		case st == 9:
			v = int64(1)
		case st == 10, st == 11:
			v = nil
		case st >= 12 && st%2 == 0:
			need = int((st - 12) / 2)
			if need < 0 || need > len(b)-vp {
				need = 0
			} else {
				blob := make([]byte, need)
				copy(blob, b[vp:vp+need])
				v = blob
			}
		case st >= 13:
			need = int((st - 13) / 2)
			if need < 0 || need > len(b)-vp {
				need = 0
			} else {
				v = string(b[vp : vp+need])
			}
		default:
			v = nil
		}
		values[i] = v
		vp += need
	}
	return values
}

// readBigEndianInt reads n (1..8) big-endian bytes as a signed integer, which
// is what serial types 1..6 mean.
func readBigEndianInt(b []byte, off, n int) int64 {
	if n <= 0 || off < 0 || off+n > len(b) {
		return 0
	}
	var v int64
	for i := 0; i < n; i++ {
		v = (v << 8) | int64(b[off+i])
	}
	return v
}

// readVarint decodes SQLite's variable-length integer: big-endian, seven bits
// per byte, at most nine bytes. It stops at the end of the buffer instead of
// running past it.
func readVarint(b []byte, pos *int) int64 {
	p := *pos
	var v int64
	i := 0
	for ; i < 8; i++ {
		if p >= len(b) {
			*pos = p
			return v
		}
		c := b[p]
		p++
		v = (v << 7) | int64(c&0x7F)
		if c&0x80 == 0 {
			*pos = p
			return v
		}
	}
	if p < len(b) {
		v = (v << 8) | int64(b[p])
		p++
	}
	*pos = p
	return v
}

func be16(b []byte, o int) int {
	if o < 0 || o+2 > len(b) {
		return 0
	}
	return int(b[o])<<8 | int(b[o+1])
}

func be32(b []byte, o int) uint32 {
	if o < 0 || o+4 > len(b) {
		return 0
	}
	return uint32(b[o])<<24 | uint32(b[o+1])<<16 | uint32(b[o+2])<<8 | uint32(b[o+3])
}

// toInt converts a decoded value to a page number, clamped to int32 so a
// corrupt rootpage cannot wrap into a plausible-looking page.
func toInt(v any) int {
	switch n := v.(type) {
	case int64:
		if n > math.MaxInt32 {
			return math.MaxInt32
		}
		if n < math.MinInt32 {
			return math.MinInt32
		}
		return int(n)
	case float64:
		if math.IsNaN(n) {
			return 0
		}
		if n > math.MaxInt32 {
			return math.MaxInt32
		}
		if n < math.MinInt32 {
			return math.MinInt32
		}
		return int(n)
	}
	return 0
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

// ---------------------------------------------------------------------------
// Queries
// ---------------------------------------------------------------------------

// Rows reads a whole table in order and materialises it. Rows are slices of
// values in table-column order; the values are nil, int64, float64, []byte or
// string, matching the C# object[] the adapters already handle.
//
// It never returns an error: an unreadable or corrupt database simply yields
// no rows.
func (db *SqliteDB) Rows(tableName string) [][]any {
	var result [][]any
	if db == nil {
		return result
	}
	t := db.Table(tableName)
	if t == nil || t.RootPage <= 0 {
		return result
	}

	for _, lp := range db.collectLeaves(t.RootPage) {
		page := db.readPage(lp)
		if page == nil {
			continue
		}
		b := headerBase(lp)
		if b+8 > len(page) || page[b] != 0x0D {
			continue
		}
		n := be16(page, b+3)
		for i := 0; i < n; i++ {
			ptr := b + 8 + i*2
			if ptr+2 > len(page) {
				break
			}
			rec := db.readLeafCell(page, be16(page, ptr))
			if rec == nil {
				continue
			}
			result = append(result, rec)
			if len(result) >= sqliteMaxRows {
				return result
			}
		}
	}
	return result
}

// SqliteText returns row[index] as a string, or "" when it is missing or not
// text. The C# equivalent returns null; callers test for emptiness either way.
func SqliteText(row []any, index int) string {
	if index < 0 || index >= len(row) {
		return ""
	}
	s, _ := row[index].(string)
	return s
}

// SqliteNumber returns row[index] as an integer, or 0 when it is missing or
// not numeric.
func SqliteNumber(row []any, index int) int64 {
	if index < 0 || index >= len(row) {
		return 0
	}
	switch v := row[index].(type) {
	case int64:
		return v
	case float64:
		if math.IsNaN(v) {
			return 0
		}
		if v >= math.MaxInt64 {
			return math.MaxInt64
		}
		if v <= math.MinInt64 {
			return math.MinInt64
		}
		return int64(v)
	}
	return 0
}

// QueryRows is the one-call entry point for adapters: it opens the database,
// looks the table up by name, and returns one map per row keyed by the
// requested column names. Columns the table does not have are omitted, and a
// key that is absent reads back as nil, so callers can use the map directly.
//
// It returns nil when the file is unreadable or is not a database, when the
// table does not exist, or when none of the requested columns exist.
func QueryRows(path, table string, columns []string) []map[string]any {
	db := OpenSqlite(path)
	if db == nil {
		return nil
	}
	defer db.Close()

	t := db.Table(table)
	if t == nil {
		return nil
	}

	idx := make([]int, len(columns))
	found := false
	for i, c := range columns {
		idx[i] = t.IndexOf(c)
		if idx[i] >= 0 {
			found = true
		}
	}
	if !found {
		return nil
	}

	rows := db.Rows(table)
	if len(rows) == 0 {
		return nil
	}

	out := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		m := make(map[string]any, len(columns))
		for i, c := range columns {
			if idx[i] >= 0 && idx[i] < len(r) {
				m[c] = r[idx[i]]
			}
		}
		out = append(out, m)
	}
	return out
}
