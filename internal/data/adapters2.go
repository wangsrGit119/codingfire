//
//  adapters2.go — CodingFire (Go)
//
//  Port of src/Data/Adapters2.cs: the multi-tool aggregation adapters.
//
//  Token accounting follows the same reference implementation the C# cites
//  (juejin-usage, MIT): each tool's four token classes are split the same way
//  and deduplicated the same way. Tools whose local logs carry no real token
//  metadata — Kiro, Trae, Cursor — are deliberately not collected; estimating
//  from character counts would turn the fire into noise.
//
//  The SQLite sources read their databases through the read-only page reader
//  in sqlite.go, which is a port of SqliteReader.cs. Nothing here executes SQL,
//  writes to a tool's directory, or touches the network.
//

package data

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wangsrGit119/codingfire/internal/core"
)

// ---------------------------------------------------------------------------
// WorkBuddy — <home>/projects/**/*.jsonl, providerData.rawUsage
//
//   The CN and INTL builds are two independent installs with separate home
//   directories (~/.workbuddy vs ~/.workbuddy-ai) and identical formats, so one
//   adapter serves both: only the source, env var, folder name and id prefix
//   differ. Recognising just ~/.workbuddy would drop the INTL usage entirely.
//
//   There is also a SQLite fallback — workbuddy.db's session_usage, which holds
//   only per-session totals with no breakdown.
// ---------------------------------------------------------------------------

type workBuddyAdapter struct {
	baseAdapter

	source     core.UsageSource
	envName    string
	folderName string
	idPrefix   string
	home       string

	// jsonlSessions are sessions that already have detail in JSONL; the
	// database fallback must not count those again.
	jsonlSessions map[string]bool
	dbBest        map[string]int
	dbPath        string
	dbLen         int64
	dbTicks       int64
}

func newWorkBuddyAdapter() *workBuddyAdapter {
	return newWorkBuddyFor(core.WorkBuddy, "WORKBUDDY_HOME", ".workbuddy", "workbuddy")
}

// newWorkBuddyIntlAdapter is the INTL install. Constructed in one place so the
// runtime and any fixture use the same configuration.
func newWorkBuddyIntlAdapter() *workBuddyAdapter {
	return newWorkBuddyFor(core.WorkBuddyIntl, "WORKBUDDY_AI_HOME", ".workbuddy-ai", "workbuddy-intl")
}

func newWorkBuddyFor(source core.UsageSource, envName, folderName, idPrefix string) *workBuddyAdapter {
	home := ""
	if env, ok := pathEnv(envName); ok {
		home = expandTilde(env)
	} else {
		home = combinePath(core.AppPaths.Home(), folderName)
	}
	return &workBuddyAdapter{
		baseAdapter:   baseAdapter{root: combinePath(home, "projects")},
		source:        source,
		envName:       envName,
		folderName:    folderName,
		idPrefix:      idPrefix,
		home:          home,
		jsonlSessions: map[string]bool{},
		dbBest:        map[string]int{},
		dbLen:         -1,
		dbTicks:       -1,
	}
}

func (a *workBuddyAdapter) dbFile() string { return combinePath(a.home, "workbuddy.db") }

// WatchRoots also covers workbuddy.db, which sits beside the projects tree
// rather than inside it.
func (a *workBuddyAdapter) WatchRoots() []string {
	return []string{a.root, a.dbFile()}
}

func (a *workBuddyAdapter) CheckConnection() (core.SourceConnectionState, string) {
	state, detail := a.baseAdapter.CheckConnection()
	if state != core.SourceNotFound {
		return state, detail
	}
	if isDir(a.home) {
		return core.SourceOk, core.AppPaths.Shorten(a.home)
	}
	return core.SourceNotFound, detail
}

func (a *workBuddyAdapter) DiscoverLogFiles(since time.Time) []string {
	return discoverJSONL(a.root, since)
}

func (a *workBuddyAdapter) ParseLine(line, filePath string) (core.UsageEvent, bool) {
	if blank(line) {
		return core.UsageEvent{}, false
	}
	obj, ok := parseJObj(line)
	if !ok {
		return core.UsageEvent{}, false
	}

	provider := obj.Obj("providerData")
	raw, ok := objField(provider, "rawUsage")
	if !ok {
		return core.UsageEvent{}, false
	}

	promptTokens := nonNeg(raw.Raw("prompt_tokens"))
	completion := nonNeg(raw.Raw("completion_tokens"))
	promptDetails := raw.Obj("prompt_tokens_details")

	// Three different spellings in the wild; the largest wins, matching the
	// reference implementation.
	cacheRead := max(nonNeg(raw.Raw("cache_read_input_tokens")),
		max(nonNeg(promptDetails.Raw("cached_tokens")),
			nonNeg(raw.Raw("prompt_cache_hit_tokens"))))
	cacheWrite := nonNeg(raw.Raw("cache_creation_input_tokens"))

	// Unlike CodeBuddy, prompt_tokens here includes the cached part.
	input := max(0, promptTokens-cacheRead-cacheWrite)
	tok := token4{input: input, output: completion, cacheRead: cacheRead, cacheWrite: cacheWrite}
	if tok.total() <= 0 {
		return core.UsageEvent{}, false
	}

	sessionID := obj.Str("sessionId")
	if sessionID == "" {
		sessionID = nameWithoutExt(filePath)
	}
	a.jsonlSessions[sessionID] = true

	// A millisecond timestamp is far outside int range: reading it as an int
	// would clamp it to int32 max and then be interpreted as seconds, landing
	// the event in 2038. It must be read as a float.
	tsRaw, hasTS := obj.Num("timestamp")
	ts := time.Now()
	if hasTS && tsRaw > 0 {
		if v, ok := parseEpochAny(tsRaw); ok {
			ts = v
		}
	}

	msgID := obj.Str("id")
	if msgID == "" {
		msgID = provider.Str("messageId")
	}
	if msgID == "" {
		if hasTS {
			msgID = sessionID + ":" + itoa64(int64(tsRaw))
		} else {
			msgID = stableHash(line)
		}
	}

	return core.UsageEvent{
		ID:        a.idPrefix + ":" + msgID,
		Source:    a.source,
		Timestamp: ts,
		Tokens:    tok.total(),
		Breakdown: tok.breakdown(),
		// On every line that carries rawUsage, providerData also names the
		// model — checked against a live log rather than assumed.
		Model:    provider.Str("model"),
		FilePath: filePath,
	}, true
}

// ParseDatabase is the session_usage fallback: it holds a running per-session
// total with no breakdown, so it is used only for sessions absent from JSONL,
// and only its growth counts — the same high-water idea Claude's streaming
// snapshots use.
func (a *workBuddyAdapter) ParseDatabase() []core.UsageEvent {
	dbPath := a.dbFile()
	if !fileExists(dbPath) {
		return nil
	}
	if a.dbPath != dbPath {
		a.dbPath, a.dbLen, a.dbTicks = dbPath, -1, -1
	}
	if !fileChanged(dbPath, &a.dbLen, &a.dbTicks) {
		return nil
	}

	db := OpenSqlite(dbPath)
	if db == nil {
		return nil
	}
	defer db.Close()

	if !db.HasTable("session_usage") {
		return nil
	}
	usage := db.Table("session_usage")
	cSID, cUsed, cUpd := usage.IndexOf("session_id"), usage.IndexOf("used"), usage.IndexOf("updated_at")
	if cSID < 0 || cUsed < 0 {
		return nil
	}

	var out []core.UsageEvent
	for _, row := range db.Rows("session_usage") {
		sid := SqliteText(row, cSID)
		if sid == "" || a.jsonlSessions[sid] {
			continue
		}
		used := int(min(SqliteNumber(row, cUsed), math.MaxInt32))
		if used <= 0 {
			continue
		}

		var upd int64
		if cUpd >= 0 {
			upd = SqliteNumber(row, cUpd)
		}
		prev := a.dbBest[sid]
		// A reset (the user cleared the session) shrinks `used`; recount from
		// zero rather than emitting a negative delta.
		if used < prev {
			prev = 0
		}
		delta := used - prev
		if delta <= 0 {
			continue
		}
		a.dbBest[sid] = used

		ts := time.Now()
		if v, ok := parseEpochAny(float64(upd)); ok {
			ts = v
		}

		id := a.idPrefix + "-db:" + sid
		if prev != 0 {
			id = a.idPrefix + "-db:" + sid + "#" + itoa(used)
		}
		out = append(out, core.UsageEvent{
			ID:          id,
			Source:      a.source,
			Timestamp:   ts,
			Tokens:      delta,
			Breakdown:   core.UsageBreakdown{Input: core.IntPtr(delta)},
			FilePath:    dbPath,
			IsEstimated: true,
		})
	}
	return out
}

// ---------------------------------------------------------------------------
// CodeBuddy — ~/.codebuddy/projects/**/*.jsonl, type == "message" && role == "assistant"
// ---------------------------------------------------------------------------

type codeBuddyAdapter struct {
	baseAdapter
	home string
}

func newCodeBuddyAdapter() *codeBuddyAdapter {
	home := ""
	if env, ok := pathEnv("CODEBUDDY_HOME"); ok {
		home = expandTilde(env)
	} else {
		home = combinePath(core.AppPaths.Home(), ".codebuddy")
	}
	return &codeBuddyAdapter{baseAdapter: baseAdapter{root: combinePath(home, "projects")}, home: home}
}

func (a *codeBuddyAdapter) CheckConnection() (core.SourceConnectionState, string) {
	state, detail := a.baseAdapter.CheckConnection()
	if state != core.SourceNotFound {
		return state, detail
	}
	if isDir(a.home) {
		return core.SourceOk, core.AppPaths.Shorten(a.home)
	}
	return core.SourceNotFound, detail
}

func (a *codeBuddyAdapter) DiscoverLogFiles(since time.Time) []string {
	return discoverJSONL(a.root, since)
}

func (a *codeBuddyAdapter) ParseLine(line, filePath string) (core.UsageEvent, bool) {
	if blank(line) {
		return core.UsageEvent{}, false
	}
	obj, ok := parseJObj(line)
	if !ok || obj.Str("type") != "message" || obj.Str("role") != "assistant" {
		return core.UsageEvent{}, false
	}

	raw, ok := objField(obj.Obj("providerData"), "rawUsage")
	if !ok {
		return core.UsageEvent{}, false
	}

	promptTokens := nonNeg(raw.Raw("prompt_tokens"))
	completion := nonNeg(raw.Raw("completion_tokens"))
	details := raw.Obj("prompt_tokens_details")

	cacheRead := max(nonNeg(details.Raw("cached_tokens")),
		nonNeg(raw.Raw("cache_read_input_tokens")))
	cacheWrite := nonNeg(raw.Raw("cache_creation_input_tokens"))
	input := max(0, promptTokens-cacheRead)
	reasoning := nonNeg(details.Raw("reasoning_tokens"))

	tok := token4{input: input, output: completion + reasoning, cacheRead: cacheRead, cacheWrite: cacheWrite}
	if tok.total() <= 0 {
		return core.UsageEvent{}, false
	}

	sessionID := obj.Str("sessionId")
	if sessionID == "" {
		sessionID = nameWithoutExt(filePath)
	}

	tsRaw, hasTS := obj.Num("timestamp")
	ts := time.Now()
	if hasTS && tsRaw > 0 {
		if v, ok := parseEpochAny(tsRaw); ok {
			ts = v
		}
	}

	msgID, ok := firstNonEmpty(obj, "uuid", "id")
	if !ok {
		if hasTS {
			msgID = sessionID + ":" + itoa64(int64(tsRaw))
		} else {
			msgID = stableHash(line)
		}
	}

	return core.UsageEvent{
		ID:        "codebuddy:" + msgID,
		Source:    core.CodeBuddy,
		Timestamp: ts,
		Tokens:    tok.total(),
		Breakdown: tok.breakdown(),
		FilePath:  filePath,
	}, true
}

// ---------------------------------------------------------------------------
// Qoder — CLI / Work session transcripts (~/.qoder/projects, ~/.qoderwork/projects)
//   plus the IDE's own SQLite ledger. Only the JSONL side and the
//   chat_message table are covered here.
// ---------------------------------------------------------------------------

type qoderAdapter struct {
	baseAdapter
	roots  []string
	ideDbs []string
	stamps fileStampCache
}

func newQoderAdapter() *qoderAdapter {
	roots := qoderRoots()
	root := ""
	if len(roots) > 0 {
		root = roots[0]
	}
	return &qoderAdapter{
		baseAdapter: baseAdapter{root: root},
		roots:       roots,
		ideDbs:      qoderIdeDbPaths(),
	}
}

// qoderIdeDbPaths lists the IDE ledgers. Both the INTL (Qoder) and CN
// (QoderCN) products write %APPDATA%\<product>\SharedClientCache\cache\db\local.db.
func qoderIdeDbPaths() []string {
	if env, ok := pathEnv("AI_USAGE_QODER_IDE_ROOTS"); ok {
		var out []string
		for _, r := range splitRoots(env) {
			p := expandTilde(r)
			out = append(out,
				p,
				combinePath(p, "SharedClientCache", "cache", "db", "local.db"),
				combinePath(p, "local.db"))
		}
		if len(out) > 0 {
			return out
		}
	}
	appData := appDataDir()
	if appData == "" {
		return nil
	}
	return []string{
		combinePath(appData, "Qoder", "SharedClientCache", "cache", "db", "local.db"),
		combinePath(appData, "QoderCN", "SharedClientCache", "cache", "db", "local.db"),
	}
}

func qoderRoots() []string {
	if env, ok := pathEnv("AI_USAGE_QODER_ROOTS"); ok {
		if raw := splitRoots(env); len(raw) > 0 {
			out := make([]string, 0, len(raw))
			for _, r := range raw {
				out = append(out, expandTilde(r))
			}
			return out
		}
	}
	return []string{
		combinePath(core.AppPaths.Home(), ".qoder", "projects"),
		combinePath(core.AppPaths.Home(), ".qoderwork", "projects"),
	}
}

func (a *qoderAdapter) CheckConnection() (core.SourceConnectionState, string) {
	for _, r := range a.roots {
		if isDir(r) {
			return core.SourceOk, core.AppPaths.Shorten(r)
		}
	}
	for _, db := range a.ideDbs {
		if fileExists(db) {
			return core.SourceOk, core.AppPaths.Shorten(db)
		}
	}
	return core.SourceNotFound, core.AppPaths.Shorten(a.root)
}

// WatchRoots covers the IDE databases too: they live outside the log tree.
func (a *qoderAdapter) WatchRoots() []string {
	out := make([]string, 0, len(a.roots)+len(a.ideDbs))
	out = append(out, a.roots...)
	out = append(out, a.ideDbs...)
	return out
}

func (a *qoderAdapter) DiscoverLogFiles(since time.Time) []string {
	var out []string
	for _, root := range a.roots {
		if !isDir(root) {
			continue
		}
		for _, f := range walkFiles(root, ".jsonl") {
			if modifiedSince(f, since) {
				out = append(out, f)
			}
		}
	}
	return out
}

func (a *qoderAdapter) ParseLine(line, filePath string) (core.UsageEvent, bool) {
	if blank(line) {
		return core.UsageEvent{}, false
	}
	obj, ok := parseJObj(line)
	if !ok {
		return core.UsageEvent{}, false
	}

	// CLI transcript: type == "assistant" with an Anthropic-shaped
	// message.usage.
	typ := obj.Str("type")
	message := obj.Obj("message")
	usage, ok := objField(message, "usage")
	if !ok {
		usage, ok = objField(obj, "usage")
	}
	if !ok {
		return core.UsageEvent{}, false
	}
	if typ != "" && typ != "assistant" && message.Str("role") != "assistant" {
		return core.UsageEvent{}, false
	}

	input := nonNeg(usage.Raw("input_tokens"))
	output := nonNeg(usage.Raw("output_tokens"))
	cacheRead := nonNeg(usage.Raw("cache_read_input_tokens"))
	cacheWrite := nonNeg(usage.Raw("cache_creation_input_tokens"))
	// Anthropic counts input_tokens net of cache.
	tok := token4{input: input, output: output, cacheRead: cacheRead, cacheWrite: cacheWrite}
	if tok.total() <= 0 {
		return core.UsageEvent{}, false
	}

	ts, ok := parseISO8601(obj.Str("timestamp"))
	if !ok {
		if v, has := obj.Num("timestamp"); has {
			ts, ok = parseEpochAny(v)
		}
	}
	if !ok {
		ts = time.Now()
	}

	msgID, ok := message.StrOk("id")
	if !ok {
		msgID, ok = firstStr(obj, "uuid", "id", "requestId")
	}
	if !ok {
		msgID = stableHash(line)
	}

	return core.UsageEvent{
		ID:        "qoder:" + msgID,
		Source:    core.Qoder,
		Timestamp: ts,
		Tokens:    tok.total(),
		Breakdown: tok.breakdown(),
		FilePath:  filePath,
	}, true
}

// ParseDatabase reads the IDE ledger: chat_message rows whose role is
// assistant carry token_info as a JSON blob. The table layout is known, so it
// is read directly; a database without the table is skipped silently.
func (a *qoderAdapter) ParseDatabase() []core.UsageEvent {
	var out []core.UsageEvent

	for _, dbPath := range a.ideDbs {
		if !fileExists(dbPath) || !a.stamps.changed(dbPath) {
			continue
		}
		db := OpenSqlite(dbPath)
		if db == nil {
			continue
		}

		t := db.Table("chat_message")
		if t == nil {
			db.Close()
			continue
		}
		cID, cRole := t.IndexOf("id"), t.IndexOf("role")
		cTok, cGmt := t.IndexOf("token_info"), t.IndexOf("gmt_create")
		if cRole < 0 || cTok < 0 {
			db.Close()
			continue
		}

		for _, row := range db.Rows("chat_message") {
			if SqliteText(row, cRole) != "assistant" {
				continue
			}
			raw := strings.TrimSpace(SqliteText(row, cTok))
			if len(raw) <= 2 {
				continue
			}
			o, ok := core.ParseJSON(raw).(map[string]any)
			if !ok {
				continue
			}
			tok := core.Of(o)

			prompt := nonNeg(tok.Raw("prompt_tokens"))
			completion := nonNeg(tok.Raw("completion_tokens"))
			cached := nonNeg(tok.Raw("cached_tokens"))
			t4 := token4{input: max(0, prompt-cached), output: completion, cacheRead: cached}
			if t4.total() <= 0 {
				continue
			}

			mid := ""
			if cID >= 0 {
				mid = SqliteText(row, cID)
			}
			if mid == "" {
				continue
			}

			var gmt int64
			if cGmt >= 0 {
				gmt = SqliteNumber(row, cGmt)
			}
			ts := time.Now()
			if v, ok := parseEpochAny(float64(gmt)); ok {
				ts = v
			}

			out = append(out, core.UsageEvent{
				ID:        "qoder-ide:" + mid,
				Source:    core.Qoder,
				Timestamp: ts,
				Tokens:    t4.total(),
				Breakdown: t4.breakdown(),
				FilePath:  dbPath,
			})
		}
		db.Close()
	}
	return out
}

// ---------------------------------------------------------------------------
// Qwen Code — ~/.qwen/tmp/<project>/chats/*.jsonl, type == "assistant"
// ---------------------------------------------------------------------------

type qwenCodeAdapter struct{ baseAdapter }

func newQwenCodeAdapter() *qwenCodeAdapter {
	tmp := ""
	if env, ok := pathEnv("QWEN_TMP_DIR"); ok {
		tmp = expandTilde(env)
	} else {
		tmp = combinePath(core.AppPaths.Home(), ".qwen", "tmp")
	}
	return &qwenCodeAdapter{baseAdapter{root: tmp}}
}

func (a *qwenCodeAdapter) DiscoverLogFiles(since time.Time) []string {
	return discoverJSONL(a.root, since)
}

func (a *qwenCodeAdapter) ParseLine(line, filePath string) (core.UsageEvent, bool) {
	if blank(line) {
		return core.UsageEvent{}, false
	}
	obj, ok := parseJObj(line)
	if !ok || obj.Str("type") != "assistant" {
		return core.UsageEvent{}, false
	}
	meta, ok := objField(obj, "usageMetadata")
	if !ok {
		return core.UsageEvent{}, false
	}

	prompt := nonNeg(meta.Raw("promptTokenCount"))
	candidates := nonNeg(meta.Raw("candidatesTokenCount"))
	cached := nonNeg(meta.Raw("cachedContentTokenCount"))
	thoughts := nonNeg(meta.Raw("thoughtsTokenCount"))

	input := max(0, prompt-cached)
	output := max(0, candidates-thoughts) + thoughts // reasoning counts as output
	tok := token4{input: input, output: output, cacheRead: cached}
	if tok.total() <= 0 {
		return core.UsageEvent{}, false
	}

	msgID, ok := firstNonEmpty(obj, "uuid", "id")
	if !ok {
		msgID = stableHash(line)
	}

	ts, ok := parseISO8601(obj.Str("timestamp"))
	if !ok {
		if v, has := obj.Num("timestamp"); has {
			ts, ok = parseEpochAny(v)
		}
	}
	if !ok {
		ts = time.Now()
	}

	return core.UsageEvent{
		ID:        "qwen:" + msgID,
		Source:    core.QwenCode,
		Timestamp: ts,
		Tokens:    tok.total(),
		Breakdown: tok.breakdown(),
		FilePath:  filePath,
	}, true
}

// ---------------------------------------------------------------------------
// Kimi — new layout ~/.kimi-code/sessions/*/wire.jsonl
//        legacy     ~/.kimi/sessions/<workDir>/<session>/wire.jsonl
// ---------------------------------------------------------------------------

type kimiAdapter struct {
	baseAdapter
	roots []string
}

func newKimiAdapter() *kimiAdapter {
	var roots []string
	if env, ok := pathEnv("KIMI_CODE_HOME"); ok {
		roots = append(roots, expandTilde(env))
	} else {
		roots = append(roots, combinePath(core.AppPaths.Home(), ".kimi-code"))
	}
	if env, ok := pathEnv("KIMI_HOME"); ok {
		roots = append(roots, expandTilde(env))
	} else {
		roots = append(roots, combinePath(core.AppPaths.Home(), ".kimi"))
	}
	return &kimiAdapter{baseAdapter: baseAdapter{root: roots[0]}, roots: roots}
}

func (a *kimiAdapter) CheckConnection() (core.SourceConnectionState, string) {
	for _, r := range a.roots {
		if isDir(r) {
			return core.SourceOk, core.AppPaths.Shorten(r)
		}
	}
	return core.SourceNotFound, core.AppPaths.Shorten(a.root)
}

func (a *kimiAdapter) DiscoverLogFiles(since time.Time) []string {
	var out []string
	for _, root := range a.roots {
		sessions := combinePath(root, "sessions")
		if !isDir(sessions) {
			continue
		}
		for _, f := range walkFiles(sessions, ".jsonl") {
			if !strings.EqualFold(filepath.Base(f), "wire.jsonl") {
				continue
			}
			if modifiedSince(f, since) {
				out = append(out, f)
			}
		}
	}
	return out
}

func (a *kimiAdapter) ParseLine(line, filePath string) (core.UsageEvent, bool) {
	if blank(line) {
		return core.UsageEvent{}, false
	}
	obj, ok := parseJObj(line)
	if !ok {
		return core.UsageEvent{}, false
	}

	// ---- current layout: step.end carries usage ----
	// The outer record is either {type:"step.end",usage:{…}} or
	// {type:"context.append_loop_event",event:{type:"step.end",usage:{…}}}.
	evt := obj
	if inner, ok := objField(obj, "event"); ok {
		evt = inner
	}
	usage, hasUsage := objField(evt, "usage")
	if evt.Str("type") == "step.end" && hasUsage {
		inputOther := nonNeg(usage.Raw("inputOther"))
		// The reference implementation uses inputCacheRead /
		// inputCacheCreation; the older cacheRead / cacheWrite spellings are
		// accepted too.
		cacheRead := nonNeg(usage.Raw("inputCacheRead"))
		if cacheRead == 0 {
			cacheRead = nonNeg(usage.Raw("cacheRead"))
		}
		cacheWrite := nonNeg(usage.Raw("inputCacheCreation"))
		if cacheWrite == 0 {
			cacheWrite = nonNeg(usage.Raw("cacheWrite"))
		}
		output := nonNeg(usage.Raw("output"))

		tok := token4{input: inputOther, output: output, cacheRead: cacheRead, cacheWrite: cacheWrite}
		if tok.total() <= 0 {
			return core.UsageEvent{}, false
		}

		ts, ok := epochFrom(evt.Num("time"))
		if !ok {
			ts, ok = epochFrom(obj.Num("time"))
		}
		if !ok {
			ts, ok = parseISO8601(obj.Str("time"))
		}
		if !ok {
			ts = time.Now()
		}

		id, ok := firstStr(evt, "uuid")
		if !ok {
			id, ok = firstStr(obj, "uuid")
		}
		if !ok {
			id = stableHash(line)
		}

		return core.UsageEvent{
			ID:        "kimi:" + id,
			Source:    core.Kimi,
			Timestamp: ts,
			Tokens:    tok.total(),
			Breakdown: tok.breakdown(),
			FilePath:  filePath,
		}, true
	}

	// ---- legacy layout: message.type == "StatusUpdate" ----
	message := obj.Obj("message")
	if message.Str("type") != "StatusUpdate" {
		return core.UsageEvent{}, false
	}
	payload := message.Obj("payload")
	tu, ok := objField(payload, "token_usage")
	if !ok {
		tu, ok = objField(message, "token_usage")
	}
	if !ok {
		tu, ok = objField(obj, "token_usage")
	}
	if !ok {
		return core.UsageEvent{}, false
	}

	// input_other is the input count itself; cache is reported separately and
	// is not subtracted.
	in := nonNeg(tu.Raw("input_other"))
	if in == 0 {
		in = nonNeg(tu.Raw("input_tokens"))
	}
	outTok := nonNeg(tu.Raw("output"))
	cRead := nonNeg(tu.Raw("input_cache_read"))
	if cRead == 0 {
		cRead = nonNeg(tu.Raw("cache_read_input_tokens"))
	}
	cWrite := nonNeg(tu.Raw("input_cache_creation"))
	if cWrite == 0 {
		cWrite = nonNeg(tu.Raw("cache_creation_input_tokens"))
	}

	tok := token4{input: in, output: outTok, cacheRead: cRead, cacheWrite: cWrite}
	if tok.total() <= 0 {
		return core.UsageEvent{}, false
	}

	// The legacy timestamp is in seconds, on the entry or on the payload.
	ts, ok := epochFrom(obj.Num("timestamp"))
	if !ok {
		ts, ok = epochFrom(payload.Num("timestamp"))
	}
	if !ok {
		ts, ok = parseISO8601(message.Str("timestamp"))
	}
	if !ok {
		ts = time.Now()
	}

	id, ok := firstNonEmpty(payload, "message_id")
	if !ok {
		id, ok = firstNonEmpty(message, "message_id")
	}
	if !ok {
		id, ok = firstNonEmpty(obj, "uuid")
	}
	if !ok {
		id = stableHash(line)
	}

	return core.UsageEvent{
		ID:        "kimi:" + id,
		Source:    core.Kimi,
		Timestamp: ts,
		Tokens:    tok.total(),
		Breakdown: tok.breakdown(),
		FilePath:  filePath,
	}, true
}

// epochFrom applies parseEpochAny to an optional numeric field.
func epochFrom(v float64, ok bool) (time.Time, bool) {
	if !ok {
		return time.Time{}, false
	}
	return parseEpochAny(v)
}

// ---------------------------------------------------------------------------
// GitHub Copilot CLI — ~/.copilot/session-state/<sid>/events.jsonl
//   Only session.shutdown carries modelMetrics, and one of those is a whole
//   session's bill.
// ---------------------------------------------------------------------------

type copilotAdapter struct {
	baseAdapter
	home string
}

func newCopilotAdapter() *copilotAdapter {
	home := ""
	if env, ok := pathEnv("COPILOT_HOME"); ok {
		home = expandTilde(env)
	} else {
		home = combinePath(core.AppPaths.Home(), ".copilot")
	}
	return &copilotAdapter{
		baseAdapter: baseAdapter{root: combinePath(home, "session-state")},
		home:        home,
	}
}

func (a *copilotAdapter) CheckConnection() (core.SourceConnectionState, string) {
	state, detail := a.baseAdapter.CheckConnection()
	if state != core.SourceNotFound {
		return state, detail
	}
	if isDir(a.home) {
		return core.SourceOk, core.AppPaths.Shorten(a.home)
	}
	return core.SourceNotFound, detail
}

func (a *copilotAdapter) DiscoverLogFiles(since time.Time) []string {
	if !isDir(a.root) {
		return nil
	}
	var out []string
	for _, f := range walkFiles(a.root, ".jsonl") {
		if !strings.EqualFold(filepath.Base(f), "events.jsonl") {
			continue
		}
		if modifiedSince(f, since) {
			out = append(out, f)
		}
	}
	return out
}

func (a *copilotAdapter) ParseLine(line, filePath string) (core.UsageEvent, bool) {
	if blank(line) {
		return core.UsageEvent{}, false
	}
	obj, ok := parseJObj(line)
	if !ok || obj.Str("type") != "session.shutdown" {
		return core.UsageEvent{}, false
	}
	metrics, ok := objField(obj, "modelMetrics")
	if !ok {
		return core.UsageEvent{}, false
	}

	sessionID := filepath.Base(dirName(filePath))
	if sessionID == "" {
		sessionID = stableHash(filePath)
	}
	stamp, hasStamp := obj.StrOk("timestamp")
	if !hasStamp {
		stamp, _ = obj.StrOk("stamp")
	}

	// One shutdown can carry several models. This app does not split its
	// accounting by model, so they are merged into a single event — ParseLine
	// can only return one, and returning early would drop the other models.
	//
	// The merge is also why the model name is kept only when there was exactly
	// one: attributing a multi-model total to whichever key happened to come
	// first would be a guess wearing the costume of data.
	var sumInput, sumOutput, sumRead, sumWrite int
	onlyModel, usedModels := "", 0
	for _, model := range metrics.Keys() {
		u, ok := objField(metrics.Obj(model), "usage")
		if !ok {
			continue
		}
		usedModels++
		onlyModel = model
		inRaw := nonNeg(u.Raw("inputTokens"))
		cRead := nonNeg(u.Raw("cacheReadTokens"))
		sumInput += max(0, inRaw-cRead)
		sumOutput += nonNeg(u.Raw("outputTokens"))
		sumRead += cRead
		sumWrite += nonNeg(u.Raw("cacheWriteTokens"))
	}

	tok := token4{input: sumInput, output: sumOutput, cacheRead: sumRead, cacheWrite: sumWrite}
	if tok.total() <= 0 {
		return core.UsageEvent{}, false
	}

	ts, ok := parseISO8601(stamp)
	if !ok {
		ts = time.Now()
	}

	if usedModels != 1 {
		onlyModel = ""
	}

	return core.UsageEvent{
		ID:        "copilot:" + sessionID + "|shutdown|" + stamp,
		Source:    core.Copilot,
		Timestamp: ts,
		Tokens:    tok.total(),
		Breakdown: tok.breakdown(),
		Model:     onlyModel,
		FilePath:  filePath,
	}, true
}

// ---------------------------------------------------------------------------
// OpenCode family (OpenCode / ZCode / Mimo / Kilo CLI share this message table)
//   message.data is JSON: role / tokens{input,output,reasoning,cache{read,write}}
//   Cumulative: a message grows as output streams, so the monitor deltas it.
// ---------------------------------------------------------------------------

type opencodeStyleAdapter struct {
	baseAdapter

	source core.UsageSource
	dbFile string
	// accept filters messages this source should not own; nil accepts all.
	accept func(core.JObj) bool

	dbLen    int64
	dbTicks  int64
	lastRead time.Time
}

func (a *opencodeStyleAdapter) IsCumulative() bool { return true }

func (a *opencodeStyleAdapter) CheckConnection() (core.SourceConnectionState, string) {
	detail := core.AppPaths.Shorten(a.dbFile)
	if fileExists(a.dbFile) {
		return core.SourceOk, detail
	}
	return core.SourceNotFound, detail
}

// DiscoverLogFiles is empty: the ledger is a database, not a log file.
func (a *opencodeStyleAdapter) DiscoverLogFiles(time.Time) []string { return nil }

func (a *opencodeStyleAdapter) ParseLine(string, string) (core.UsageEvent, bool) {
	return core.UsageEvent{}, false
}

func (a *opencodeStyleAdapter) ParseDatabase() []core.UsageEvent {
	path := a.dbFile
	if !fileExists(path) {
		return nil
	}
	// The database can be tens of megabytes; a full re-read every four seconds
	// is far too expensive. Skip when nothing changed, and even then re-read
	// at most every eight seconds.
	if !fileChanged(path, &a.dbLen, &a.dbTicks) {
		return nil
	}
	if time.Since(a.lastRead).Seconds() < 8 {
		return nil
	}
	a.lastRead = time.Now()

	db := OpenSqlite(path)
	if db == nil {
		return nil
	}
	defer db.Close()

	t := db.Table("message")
	if t == nil {
		return nil
	}
	cData, cID, cSID := t.IndexOf("data"), t.IndexOf("id"), t.IndexOf("session_id")
	if cData < 0 {
		return nil
	}

	var out []core.UsageEvent
	for _, row := range db.Rows("message") {
		text := SqliteText(row, cData)
		if text == "" {
			continue
		}
		m, ok := core.ParseJSON(text).(map[string]any)
		if !ok {
			continue
		}
		data := core.Of(m)
		if data.Str("role") != "assistant" {
			continue
		}
		if a.accept != nil && !a.accept(data) {
			continue
		}

		tok := opencodeTokens(data.Obj("tokens"))
		if tok.total() <= 0 {
			continue
		}

		sid := ""
		if cSID >= 0 {
			sid = SqliteText(row, cSID)
		}
		if sid == "" {
			sid = data.Str("sessionID")
		}
		mid := ""
		if cID >= 0 {
			mid = SqliteText(row, cID)
		}
		if mid == "" {
			mid = data.Str("id")
		}
		if mid == "" {
			continue
		}

		tm := data.Obj("time")
		ts, ok := epochFrom(tm.Num("completed"))
		if !ok {
			ts, ok = epochFrom(tm.Num("created"))
		}
		if !ok {
			ts = time.Now()
		}

		if sid == "" {
			sid = "?"
		}
		out = append(out, core.UsageEvent{
			ID:        a.source.Raw() + ":" + sid + "|" + mid,
			Source:    a.source,
			Timestamp: ts,
			Tokens:    tok.total(),
			Breakdown: tok.breakdown(),
			FilePath:  path,
		})
	}
	return out
}

// opencodeTokens is the shared tokens shape of the OpenCode family.
func opencodeTokens(tokens core.JObj) token4 {
	if tokens.Len() == 0 {
		return token4{}
	}
	input := nonNeg(tokens.Raw("input"))
	output := nonNeg(tokens.Raw("output"))
	reasoning := nonNeg(tokens.Raw("reasoning"))
	cache := tokens.Obj("cache")
	cRead := nonNeg(cache.Raw("read"))
	cWrite := nonNeg(cache.Raw("write"))
	// reasoning counts as output, matching every other source's billing.
	return token4{input: input, output: output + reasoning, cacheRead: cRead, cacheWrite: cWrite}
}

// ---------------------------------------------------------------------------
// OpenCode — ~/.local/share/opencode/opencode.db
//   (older versions also keep loose JSON under storage/message)
// ---------------------------------------------------------------------------

type openCodeAdapter struct {
	*opencodeStyleAdapter
	stamps fileStampCache
}

func newOpenCodeAdapter() *openCodeAdapter {
	return &openCodeAdapter{opencodeStyleAdapter: &opencodeStyleAdapter{
		baseAdapter: baseAdapter{root: combinePath(openCodeDataDir(), "opencode.db")},
		source:      core.OpenCode,
		dbFile:      combinePath(openCodeDataDir(), "opencode.db"),
		dbLen:       -1,
		dbTicks:     -1,
	}}
}

func openCodeDataDir() string {
	if env, ok := pathEnv("OPENCODE_HOME"); ok {
		return expandTilde(env)
	}
	if xdg, ok := pathEnv("XDG_DATA_HOME"); ok {
		return combinePath(expandTilde(xdg), "opencode")
	}
	return combinePath(core.AppPaths.Home(), ".local", "share", "opencode")
}

// DiscoverLogFiles falls back to the loose JSON layout only when the database
// is absent — the database is authoritative, and reading both would count the
// same usage twice.
func (a *openCodeAdapter) DiscoverLogFiles(since time.Time) []string {
	if fileExists(a.dbFile) {
		return nil
	}
	root := combinePath(openCodeDataDir(), "storage", "message")
	if !isDir(root) {
		return nil
	}
	var out []string
	for _, f := range walkFiles(root, ".json") {
		if modifiedSince(f, since) {
			out = append(out, f)
		}
	}
	return out
}

// ParseThread reads one loose message file. It always claims the file (the
// result is non-nil even when empty), because there is no line format to fall
// back to.
func (a *openCodeAdapter) ParseThread(filePath string) []core.UsageEvent {
	out := []core.UsageEvent{}
	if fileExists(a.dbFile) {
		return out // the database wins; ignore the legacy layout
	}
	if !a.stamps.changed(filePath) {
		return out
	}

	text, ok := readAllTextShared(filePath)
	if !ok {
		return out
	}
	m, ok := core.ParseJSON(text).(map[string]any)
	if !ok {
		return out
	}
	data := core.Of(m)
	if data.Str("role") != "assistant" {
		return out
	}

	tok := opencodeTokens(data.Obj("tokens"))
	if tok.total() <= 0 {
		return out
	}

	sid := data.Str("sessionID")
	if sid == "" {
		sid = data.Str("sessionId")
	}
	if sid == "" {
		sid = filepath.Base(dirName(filePath))
	}
	mid := data.Str("id")
	if mid == "" {
		mid = nameWithoutExt(filePath)
	}

	tm := data.Obj("time")
	ts, ok := epochFrom(tm.Num("completed"))
	if !ok {
		ts, ok = epochFrom(tm.Num("created"))
	}
	if !ok {
		ts = time.Now()
	}

	if sid == "" {
		sid = "?"
	}
	out = append(out, core.UsageEvent{
		ID:        "opencode:" + sid + "|" + mid,
		Source:    core.OpenCode,
		Timestamp: ts,
		Tokens:    tok.total(),
		Breakdown: tok.breakdown(),
		FilePath:  filePath,
	})
	return out
}

// ---------------------------------------------------------------------------
// ZCode — $ZCODE_HOME/cli/db/db.sqlite (default ~/.zcode), same shape as
//   OpenCode. It embeds Claude/Codex/Gemini sub-agents and writes their
//   messages into the same table; those are counted by their own sources, so
//   they are excluded here by providerID.
// ---------------------------------------------------------------------------

type zCodeAdapter struct{ *opencodeStyleAdapter }

func newZCodeAdapter() *zCodeAdapter {
	home := ""
	if env, ok := pathEnv("ZCODE_HOME"); ok {
		home = expandTilde(env)
	} else {
		home = combinePath(core.AppPaths.Home(), ".zcode")
	}
	dbFile := combinePath(home, "cli", "db", "db.sqlite")
	return &zCodeAdapter{opencodeStyleAdapter: &opencodeStyleAdapter{
		baseAdapter: baseAdapter{root: dbFile},
		source:      core.ZCode,
		dbFile:      dbFile,
		accept:      zcodeAcceptMessage,
		dbLen:       -1,
		dbTicks:     -1,
	}}
}

func zcodeAcceptMessage(data core.JObj) bool {
	provider := strings.ToLower(data.Str("providerID"))
	if provider == "" {
		return false
	}
	if strings.Contains(provider, "anthropic") ||
		strings.Contains(provider, "openai") ||
		strings.Contains(provider, "google") {
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// Gemini CLI — ~/.gemini/tmp/<project>/chats/session-*.json (whole file) and
//   *.jsonl
// ---------------------------------------------------------------------------

type geminiCliAdapter struct {
	baseAdapter
	stamps fileStampCache
}

func newGeminiCliAdapter() *geminiCliAdapter {
	home := ""
	if env, ok := pathEnv("GEMINI_HOME"); ok {
		home = expandTilde(env)
	} else {
		home = combinePath(core.AppPaths.Home(), ".gemini")
	}
	return &geminiCliAdapter{baseAdapter: baseAdapter{root: combinePath(home, "tmp")}}
}

func (a *geminiCliAdapter) IsCumulative() bool { return true }

func (a *geminiCliAdapter) DiscoverLogFiles(since time.Time) []string {
	if !isDir(a.root) {
		return nil
	}
	out := discoverJSONL(a.root, since)
	// Whole-file sessions (session-*.json) are listed too: the monitor uses
	// ParseThread's result when there is one and falls back to line reading
	// otherwise.
	for _, f := range walkFiles(a.root, ".json") {
		if modifiedSince(f, since) {
			out = append(out, f)
		}
	}
	return out
}

// ParseThread reads a session archive: one file holding every message, with
// tokens{cached,input,output,thoughts,tool,total} on the assistant side. User
// messages have no such field and are skipped. An old session with no usage at
// all is recorded as zero — never estimated from character counts.
func (a *geminiCliAdapter) ParseThread(filePath string) []core.UsageEvent {
	out := []core.UsageEvent{}

	// Anything that is not .json (i.e. the one-record-per-line .jsonl) must
	// return nil so the monitor falls back to line reading. Returning an empty
	// slice would claim the file and those sessions would never be read.
	if !strings.EqualFold(filepath.Ext(filePath), ".json") {
		return nil
	}
	if !a.stamps.changed(filePath) {
		return out
	}

	text, ok := readAllTextShared(filePath)
	if !ok {
		return out
	}
	m, ok := core.ParseJSON(text).(map[string]any)
	if !ok {
		return out
	}
	root := core.Of(m)

	messages := root.Arr("messages")
	if messages.Len() == 0 {
		messages = root.Arr("history")
	}
	if messages.Len() == 0 {
		return out
	}

	sessionID := root.Str("sessionId")
	if sessionID == "" {
		sessionID = nameWithoutExt(filePath)
	}

	for i := 0; i < messages.Len(); i++ {
		msg := messages.ObjAt(i)
		if msg.Len() == 0 {
			continue
		}
		tok := geminiMessageTokens(msg)
		if tok.total() <= 0 {
			continue
		}

		mid := msg.Str("id")
		if mid == "" {
			mid = sessionID + ":" + itoa(i)
		}

		ts, ok := parseISO8601(msg.Str("timestamp"))
		if !ok {
			ts, ok = parseISO8601(root.Str("startTime"))
		}
		if !ok {
			ts = time.Now()
		}

		out = append(out, core.UsageEvent{
			ID:        "gemini:" + mid,
			Source:    core.GeminiCli,
			Timestamp: ts,
			Tokens:    tok.total(),
			Breakdown: tok.breakdown(),
			FilePath:  filePath,
		})
	}
	return out
}

func (a *geminiCliAdapter) ParseLine(line, filePath string) (core.UsageEvent, bool) {
	if blank(line) {
		return core.UsageEvent{}, false
	}
	obj, ok := parseJObj(line)
	if !ok {
		return core.UsageEvent{}, false
	}
	tok := geminiMessageTokens(obj)
	if tok.total() <= 0 {
		return core.UsageEvent{}, false
	}

	id, ok := firstNonEmpty(obj, "uuid", "id")
	if !ok {
		id = stableHash(line)
	}
	ts, ok := parseISO8601(obj.Str("timestamp"))
	if !ok {
		ts = time.Now()
	}

	return core.UsageEvent{
		ID:        "gemini:" + id,
		Source:    core.GeminiCli,
		Timestamp: ts,
		Tokens:    tok.total(),
		Breakdown: tok.breakdown(),
		FilePath:  filePath,
	}, true
}

// geminiMessageTokens reads either of the two shapes seen in the wild:
// messages[].tokens and usageMetadata.
func geminiMessageTokens(m core.JObj) token4 {
	if t, ok := objField(m, "tokens"); ok {
		ti := nonNeg(t.Raw("input"))
		to := nonNeg(t.Raw("output"))
		tool := nonNeg(t.Raw("tool"))
		cached := nonNeg(t.Raw("cached"))
		thoughts := nonNeg(t.Raw("thoughts"))
		return token4{input: ti, output: to + tool + thoughts, cacheRead: cached}
	}

	u, ok := objField(m, "usageMetadata")
	if !ok {
		u, ok = objField(m, "usage")
	}
	if !ok {
		return token4{}
	}
	prompt := nonNeg(u.Raw("promptTokenCount"))
	cand := nonNeg(u.Raw("candidatesTokenCount"))
	cached := nonNeg(u.Raw("cachedContentTokenCount"))
	thoughts := nonNeg(u.Raw("thoughtsTokenCount"))
	return token4{
		input:     max(0, prompt-cached),
		output:    max(0, cand-thoughts) + thoughts,
		cacheRead: cached,
	}
}

// ---------------------------------------------------------------------------
// Droid (Factory) — ~/.factory/sessions/**/*.settings.json, a whole file with
//   a running tokenUsage
// ---------------------------------------------------------------------------

type droidAdapter struct {
	baseAdapter
	stamps fileStampCache
}

func newDroidAdapter() *droidAdapter {
	dir := ""
	if env, ok := pathEnv("DROID_SESSIONS_DIR"); ok {
		dir = expandTilde(env)
	} else if fac, ok := pathEnv("FACTORY_DIR"); ok {
		dir = combinePath(expandTilde(fac), "sessions")
	} else {
		dir = combinePath(core.AppPaths.Home(), ".factory", "sessions")
	}
	return &droidAdapter{baseAdapter: baseAdapter{root: dir}}
}

func (a *droidAdapter) IsCumulative() bool { return true }

func (a *droidAdapter) DiscoverLogFiles(since time.Time) []string {
	if !isDir(a.root) {
		return nil
	}
	var out []string
	for _, f := range walkFiles(a.root, ".json") {
		if !strings.HasSuffix(strings.ToLower(f), ".settings.json") {
			continue
		}
		if modifiedSince(f, since) {
			out = append(out, f)
		}
	}
	return out
}

func (a *droidAdapter) ParseThread(filePath string) []core.UsageEvent {
	out := []core.UsageEvent{}
	if !a.stamps.changed(filePath) {
		return out
	}
	text, ok := readAllTextShared(filePath)
	if !ok {
		return out
	}
	m, ok := core.ParseJSON(text).(map[string]any)
	if !ok {
		return out
	}
	tu, ok := objField(core.Of(m), "tokenUsage")
	if !ok {
		return out
	}

	input := nonNeg(tu.Raw("inputTokens"))
	output := nonNeg(tu.Raw("outputTokens"))
	cRead := nonNeg(tu.Raw("cacheReadTokens"))
	cWrite := nonNeg(tu.Raw("cacheCreationTokens"))
	thinking := nonNeg(tu.Raw("thinkingTokens"))
	total := nonNeg(tu.Raw("totalTokens"))

	tok := token4{input: input, output: output + thinking, cacheRead: cRead, cacheWrite: cWrite}
	if tok.total() <= 0 && total <= 0 {
		return out
	}

	// The session directory name is the session identity.
	sid := filepath.Base(dirName(filePath))
	if sid == "" {
		sid = stableHash(filePath)
	}

	ts := time.Now()
	if fi, err := os.Stat(filePath); err == nil {
		ts = fi.ModTime()
	}

	tokens := tok.total()
	if total > 0 {
		tokens = total
	}
	out = append(out, core.UsageEvent{
		ID:        "droid:" + sid,
		Source:    core.Droid,
		Timestamp: ts,
		Tokens:    tokens,
		Breakdown: tok.breakdown(),
		FilePath:  filePath,
	})
	return out
}

// ---------------------------------------------------------------------------
// VS Code extensions (Cline / Roo Code / Kilo Code)
//   All three live in <host>\User\globalStorage\<extension id>\tasks\<task id>\
//   ui_messages.json, in the "say: api_req_started" text of which one request's
//   token detail is embedded.
// ---------------------------------------------------------------------------

type vscodeTaskAdapter struct {
	baseAdapter

	source      core.UsageSource
	extensionID string
	envOverride string
	roots       []string
	stamps      fileStampCache
}

var vscodeHosts = []string{
	"Code", "Code - Insiders", "VSCodium", "Cursor", "Windsurf",
	"Trae", "Trae CN", "CodeBuddy",
}

func newVscodeTaskAdapter(source core.UsageSource, extensionID, envOverride string) *vscodeTaskAdapter {
	roots := vscodeTaskRoots(extensionID, envOverride)
	root := ""
	if len(roots) > 0 {
		root = roots[0]
	}
	return &vscodeTaskAdapter{
		baseAdapter: baseAdapter{root: root},
		source:      source,
		extensionID: extensionID,
		envOverride: envOverride,
		roots:       roots,
	}
}

func vscodeTaskRoots(extensionID, envOverride string) []string {
	env, ok := pathEnv(envOverride)
	if !ok {
		env, ok = pathEnv("AI_USAGE_VSCODE_ROOTS")
	}

	var roots []string
	if ok {
		for _, r := range splitRoots(env) {
			// Accept either <host>\User\globalStorage or the tasks directory
			// itself.
			p := expandTilde(r)
			roots = append(roots, p, combinePath(p, extensionID, "tasks"))
		}
		return roots
	}

	appData := appDataDir()
	if appData == "" {
		return nil
	}
	for _, host := range vscodeHosts {
		roots = append(roots,
			combinePath(appData, host, "User", "globalStorage", extensionID, "tasks"),
			combinePath(appData, host, "User", "globalStorage", extensionID))
	}
	return roots
}

func (a *vscodeTaskAdapter) CheckConnection() (core.SourceConnectionState, string) {
	for _, r := range a.roots {
		if isDir(r) {
			d := dirName(r)
			if d == "" {
				d = r
			}
			return core.SourceOk, core.AppPaths.Shorten(d)
		}
	}
	return core.SourceNotFound, core.AppPaths.Shorten(a.root)
}

func (a *vscodeTaskAdapter) DiscoverLogFiles(since time.Time) []string {
	var out []string
	for _, root := range a.roots {
		if !isDir(root) {
			continue
		}
		// When the user points straight at a tasks directory the path contains
		// no extension id, so the whole tree counts. Otherwise the path must
		// show this extension's id — without that check, feeding in the
		// globalStorage root would make all three extensions claim each
		// other's ui_messages.json.
		trimmed := strings.TrimRight(root, `/\`)
		bare := strings.EqualFold(filepath.Base(trimmed), "tasks")

		for _, f := range walkFiles(root, ".json") {
			if !strings.EqualFold(filepath.Base(f), "ui_messages.json") {
				continue
			}
			if !bare && !strings.Contains(strings.ToLower(f), strings.ToLower(a.extensionID)) {
				continue
			}
			if modifiedSince(f, since) {
				out = append(out, f)
			}
		}
	}
	return out
}

func (a *vscodeTaskAdapter) ParseThread(filePath string) []core.UsageEvent {
	out := []core.UsageEvent{}
	if !a.stamps.changed(filePath) {
		return out
	}
	text, ok := readAllTextShared(filePath)
	if !ok {
		return out
	}
	arr, ok := core.ParseJSON(text).([]any)
	if !ok {
		return out
	}
	entries := core.ArrOf(arr)

	taskID := filepath.Base(dirName(filePath))
	if taskID == "" {
		taskID = stableHash(filePath)
	}

	for i := 0; i < entries.Len(); i++ {
		e := entries.ObjAt(i)
		if e.Len() == 0 {
			continue
		}
		if e.Str("type") != "say" || e.Str("say") != "api_req_started" {
			continue
		}

		payloadText := e.Str("text")
		if payloadText == "" {
			continue
		}
		payload, ok := core.ParseJSON(strings.TrimSpace(payloadText)).(map[string]any)
		if !ok {
			continue
		}
		p := core.Of(payload)

		tok := token4{
			input:      nonNeg(p.Raw("tokensIn")),
			output:     nonNeg(p.Raw("tokensOut")),
			cacheRead:  nonNeg(p.Raw("cacheReads")),
			cacheWrite: nonNeg(p.Raw("cacheWrites")),
		}
		if tok.total() <= 0 {
			continue
		}

		stamp, hasStamp := e.StrOk("ts")
		ts, ok := parseISO8601(stamp)
		if !ok {
			if v, has := e.Num("ts"); has {
				ts, ok = parseEpochAny(v)
			}
		}
		if !ok {
			ts = time.Now()
		}

		suffix := stamp
		if !hasStamp {
			suffix = itoa(i)
		}

		out = append(out, core.UsageEvent{
			ID:        a.source.Raw() + ":" + taskID + ":" + suffix,
			Source:    a.source,
			Timestamp: ts,
			Tokens:    tok.total(),
			Breakdown: tok.breakdown(),
			FilePath:  filePath,
		})
	}
	return out
}

type clineAdapter struct{ *vscodeTaskAdapter }

func newClineAdapter() *clineAdapter {
	return &clineAdapter{newVscodeTaskAdapter(core.Cline, "saoudrizwan.claude-dev", "AI_USAGE_CLINE_ROOTS")}
}

type rooCodeAdapter struct{ *vscodeTaskAdapter }

func newRooCodeAdapter() *rooCodeAdapter {
	return &rooCodeAdapter{newVscodeTaskAdapter(core.RooCode, "rooveterinaryinc.roo-cline", "AI_USAGE_ROOCODE_ROOTS")}
}

type kiloCodeAdapter struct{ *vscodeTaskAdapter }

func newKiloCodeAdapter() *kiloCodeAdapter {
	return &kiloCodeAdapter{newVscodeTaskAdapter(core.KiloCode, "kilocode.kilo-code", "AI_USAGE_KILOCODE_ROOTS")}
}

// ---------------------------------------------------------------------------
// DeepSeek Harness — ~/.dsh/sessions/<ws>/<sid>/session.jsonl
//   (.jsonl.zstd variants need a zstd decoder, which this project does not
//   carry, so only the plain-text file is read)
// ---------------------------------------------------------------------------

type deepSeekHarnessAdapter struct{ baseAdapter }

func newDeepSeekHarnessAdapter() *deepSeekHarnessAdapter {
	home := ""
	if env, ok := pathEnv("DSH_HOME"); ok {
		home = expandTilde(env)
	} else {
		home = combinePath(core.AppPaths.Home(), ".dsh")
	}
	return &deepSeekHarnessAdapter{baseAdapter{root: combinePath(home, "sessions")}}
}

func (a *deepSeekHarnessAdapter) IsCumulative() bool { return true }

func (a *deepSeekHarnessAdapter) DiscoverLogFiles(since time.Time) []string {
	return discoverJSONL(a.root, since)
}

func (a *deepSeekHarnessAdapter) ParseLine(line, filePath string) (core.UsageEvent, bool) {
	if blank(line) {
		return core.UsageEvent{}, false
	}
	obj, ok := parseJObj(line)
	if !ok {
		return core.UsageEvent{}, false
	}
	if typ := obj.Str("type"); typ != "assistant" && typ != "message" {
		return core.UsageEvent{}, false
	}

	data := obj.Obj("data")
	usage, ok := objField(data, "usage")
	if !ok {
		usage, ok = objField(obj, "usage")
	}
	if !ok {
		return core.UsageEvent{}, false
	}

	// The reference implementation (dsh.ts) treats inputTokens as already
	// excluding cache, so it is not subtracted; reasoningTokens is listed
	// separately and does not enter the total; the total is the larger of the
	// reported value and the sum of the four classes.
	tok := token4{
		input:      nonNeg(usage.Raw("inputTokens")),
		output:     nonNeg(usage.Raw("outputTokens")),
		cacheRead:  nonNeg(usage.Raw("cacheReadTokens")),
		cacheWrite: nonNeg(usage.Raw("cacheWriteTokens")),
	}
	dshTotal := max(nonNeg(usage.Raw("totalTokens")), tok.total())
	if dshTotal <= 0 {
		return core.UsageEvent{}, false
	}

	tsRaw := data.Raw("time")
	if tsRaw == nil {
		tsRaw = obj.Raw("time")
	}
	ts, ok := epochOrISO(tsRaw)
	if !ok {
		ts = time.Now()
	}

	mid, ok := firstStr(data, "id")
	if !ok {
		mid, ok = firstStr(obj, "id")
	}
	if !ok {
		mid = stableHash(line)
	}

	return core.UsageEvent{
		ID:        "dsh:" + filepath.Base(dirName(filePath)) + "|" + mid,
		Source:    core.DeepSeekHarness,
		Timestamp: ts,
		Tokens:    dshTotal,
		Breakdown: tok.breakdown(),
		FilePath:  filePath,
	}, true
}

// ---------------------------------------------------------------------------
// Command Code — ~/.commandcode/projects/**/*.jsonl, type == "message" with an
//   assistant role
// ---------------------------------------------------------------------------

type commandCodeAdapter struct{ baseAdapter }

func newCommandCodeAdapter() *commandCodeAdapter {
	root := ""
	if env, ok := pathEnv("AI_USAGE_COMMANDCODE_ROOTS"); ok {
		if parts := splitRoots(env); len(parts) > 0 {
			root = expandTilde(parts[0])
		}
	}
	if root == "" {
		root = combinePath(core.AppPaths.Home(), ".commandcode", "projects")
	}
	return &commandCodeAdapter{baseAdapter{root: root}}
}

func (a *commandCodeAdapter) IsCumulative() bool { return true }

func (a *commandCodeAdapter) DiscoverLogFiles(since time.Time) []string {
	return discoverJSONL(a.root, since)
}

func (a *commandCodeAdapter) ParseLine(line, filePath string) (core.UsageEvent, bool) {
	if blank(line) {
		return core.UsageEvent{}, false
	}
	obj, ok := parseJObj(line)
	if !ok || obj.Str("type") != "message" {
		return core.UsageEvent{}, false
	}
	message := obj.Obj("message")
	if message.Str("role") != "assistant" {
		return core.UsageEvent{}, false
	}

	usage, ok := objField(message, "usage")
	if !ok {
		usage, ok = objField(obj, "usage")
	}
	if !ok {
		return core.UsageEvent{}, false
	}

	inRaw := nonNeg(usage.Raw("inputTokens"))
	if inRaw == 0 {
		inRaw = nonNeg(usage.Raw("input_tokens"))
	}
	output := nonNeg(usage.Raw("outputTokens"))
	if output == 0 {
		output = nonNeg(usage.Raw("output_tokens"))
	}
	cRead := nonNeg(usage.Raw("cacheReadTokens"))
	if cRead == 0 {
		cRead = nonNeg(usage.Raw("cache_read_input_tokens"))
	}
	cWrite := nonNeg(usage.Raw("cacheWriteTokens"))
	if cWrite == 0 {
		cWrite = nonNeg(usage.Raw("cache_creation_input_tokens"))
	}
	input := inRaw
	if cRead > 0 {
		input = max(0, inRaw-cRead)
	}

	tok := token4{input: input, output: output, cacheRead: cRead, cacheWrite: cWrite}
	if tok.total() <= 0 {
		return core.UsageEvent{}, false
	}

	mid, ok := firstStr(message, "id")
	if !ok {
		mid, ok = firstStr(obj, "uuid", "id")
	}
	if !ok {
		mid = stableHash(line)
	}

	ts, ok := parseISO8601(obj.Str("timestamp"))
	if !ok {
		if v, has := obj.Num("timestamp"); has {
			ts, ok = parseEpochAny(v)
		}
	}
	if !ok {
		ts = time.Now()
	}

	return core.UsageEvent{
		ID:        "command-code:" + mid,
		Source:    core.CommandCode,
		Timestamp: ts,
		Tokens:    tok.total(),
		Breakdown: tok.breakdown(),
		FilePath:  filePath,
	}, true
}

// ---------------------------------------------------------------------------
// OpenClaw / AutoClaw — <state dir>/agents/<id>/sessions/*.jsonl
//   The record is type == "message" with msg.role == "assistant" and a usage
//   object, but the field names vary a lot (input / inputTokens / prompt_tokens
//   …), so keys are taken leniently.
// ---------------------------------------------------------------------------

type clawStyleAdapter struct {
	baseAdapter

	source    core.UsageSource
	idPrefix  string
	stateDirs []string
}

func newClawStyleAdapter(source core.UsageSource, idPrefix string, stateDirs []string) *clawStyleAdapter {
	root := ""
	if len(stateDirs) > 0 {
		root = stateDirs[0]
	}
	return &clawStyleAdapter{
		baseAdapter: baseAdapter{root: root},
		source:      source,
		idPrefix:    idPrefix,
		stateDirs:   stateDirs,
	}
}

func (a *clawStyleAdapter) CheckConnection() (core.SourceConnectionState, string) {
	for _, r := range a.stateDirs {
		if isDir(r) {
			return core.SourceOk, core.AppPaths.Shorten(r)
		}
	}
	return core.SourceNotFound, core.AppPaths.Shorten(a.root)
}

func (a *clawStyleAdapter) DiscoverLogFiles(since time.Time) []string {
	var out []string
	for _, dir := range a.stateDirs {
		agents := combinePath(dir, "agents")
		if !isDir(agents) {
			continue
		}
		for _, f := range walkFiles(agents, ".jsonl") {
			if modifiedSince(f, since) {
				out = append(out, f)
			}
		}
	}
	return out
}

func (a *clawStyleAdapter) ParseLine(line, filePath string) (core.UsageEvent, bool) {
	if blank(line) {
		return core.UsageEvent{}, false
	}
	obj, ok := parseJObj(line)
	if !ok || obj.Str("type") != "message" {
		return core.UsageEvent{}, false
	}

	msg, ok := objField(obj, "msg")
	if !ok {
		msg, ok = objField(obj, "message")
	}
	if !ok || msg.Str("role") != "assistant" {
		return core.UsageEvent{}, false
	}

	usage, ok := objField(msg, "usage")
	if !ok {
		usage, ok = objField(obj, "usage")
	}
	if !ok {
		return core.UsageEvent{}, false
	}

	inRaw := firstOf(usage, "input", "inputTokens", "input_tokens", "promptTokens", "prompt_tokens")
	output := firstOf(usage, "output", "outputTokens", "output_tokens", "completionTokens", "completion_tokens")
	cRead := firstOf(usage, "cacheRead", "cache_read", "cachedInputTokens", "cached_input_tokens", "cache_read_input_tokens")
	cWrite := firstOf(usage, "cacheWrite", "cache_write", "cache_creation_input_tokens")
	input := inRaw
	if cRead > 0 {
		input = max(0, inRaw-cRead)
	}

	tok := token4{input: input, output: output, cacheRead: cRead, cacheWrite: cWrite}
	if tok.total() <= 0 {
		return core.UsageEvent{}, false
	}

	mid, ok := firstStr(msg, "id")
	if !ok {
		mid, ok = firstStr(obj, "id")
	}
	if !ok {
		mid = stableHash(line)
	}

	tsRaw := obj.Raw("timestamp")
	if tsRaw == nil {
		tsRaw = msg.Raw("timestamp")
	}
	ts, ok := epochOrISO(tsRaw)
	if !ok {
		ts = time.Now()
	}

	// agents/<agentId> is the owner.
	agentID := filepath.Base(dirName(dirName(filePath)))
	if agentID == "" {
		agentID = "?"
	}

	return core.UsageEvent{
		ID:        a.idPrefix + ":" + agentID + "|" + mid,
		Source:    a.source,
		Timestamp: ts,
		Tokens:    tok.total(),
		Breakdown: tok.breakdown(),
		FilePath:  filePath,
	}, true
}

// firstOf returns the first key that is present at all, whatever its value.
func firstOf(o core.JObj, keys ...string) int {
	for _, k := range keys {
		if v := o.Raw(k); v != nil {
			return nonNeg(v)
		}
	}
	return 0
}

type openClawAdapter struct{ *clawStyleAdapter }

func newOpenClawAdapter() *openClawAdapter {
	return &openClawAdapter{newClawStyleAdapter(core.OpenClaw, "openclaw", openClawStateDirs())}
}

func openClawStateDirs() []string {
	if env, ok := pathEnv("OPENCLAW_STATE_DIR"); ok {
		if one := splitRoots(env); len(one) > 0 {
			return []string{expandTilde(one[0])}
		}
	}
	home := core.AppPaths.Home()
	return []string{
		combinePath(home, ".openclaw"),
		combinePath(home, ".clawdbot"),
		combinePath(home, ".moltbot"),
		combinePath(home, ".autoclaw"),
		combinePath(home, ".openclaw-autoclaw"),
	}
}

// ---------------------------------------------------------------------------
// Every Code — <CODE_HOME>/sessions and archived_sessions, Codex rollout format
// ---------------------------------------------------------------------------

type everyCodeAdapter struct {
	baseAdapter
	home string
	// bestTotals backs the total_token_usage fallback: the largest running
	// total already counted, per session.
	bestTotals map[string]int
}

func newEveryCodeAdapter() *everyCodeAdapter {
	home := ""
	if env, ok := pathEnv("AI_USAGE_EVERY_CODE_HOME"); ok {
		home = expandTilde(env)
	} else if env, ok := pathEnv("CODE_HOME"); ok {
		home = expandTilde(env)
	} else {
		home = combinePath(core.AppPaths.Home(), ".code")
	}
	return &everyCodeAdapter{
		baseAdapter: baseAdapter{root: combinePath(home, "sessions")},
		home:        home,
		bestTotals:  map[string]int{},
	}
}

func (a *everyCodeAdapter) DiscoverLogFiles(since time.Time) []string {
	var out []string
	for _, sub := range []string{"sessions", "archived_sessions"} {
		root := combinePath(a.home, sub)
		if !isDir(root) {
			continue
		}
		for _, f := range walkFiles(root, ".jsonl") {
			if modifiedSince(f, since) {
				out = append(out, f)
			}
		}
	}
	return out
}

func (a *everyCodeAdapter) ParseLine(line, filePath string) (core.UsageEvent, bool) {
	if blank(line) {
		return core.UsageEvent{}, false
	}
	obj, ok := parseJObj(line)
	if !ok {
		return core.UsageEvent{}, false
	}

	// Two outer spellings: payload.type == "token_count" and
	// payload.msg.type == "token_count".
	payload, ok := objField(obj, "payload")
	if !ok {
		return core.UsageEvent{}, false
	}
	info, infoOK := objField(payload, "info")
	if payload.Str("type") != "token_count" || !infoOK {
		msg, msgOK := objField(payload, "msg")
		if !msgOK || msg.Str("type") != "token_count" {
			return core.UsageEvent{}, false
		}
		info, infoOK = objField(msg, "info")
		if !infoOK {
			return core.UsageEvent{}, false
		}
	}

	uuid := payload.Str("session_id")
	if uuid == "" {
		uuid = obj.Str("session_id")
	}
	if uuid == "" {
		uuid = nameWithoutExt(filePath)
	}
	stamp, hasStamp := payload.StrOk("timestamp")
	if !hasStamp {
		stamp, _ = obj.StrOk("timestamp")
	}

	// last_token_usage is this turn's increment and is taken at face value.
	// Only when it is absent is total_token_usage used, minus what this session
	// had already accumulated — reading total_token_usage unconditionally
	// counted the whole session again on every line.
	fromTotal := false
	usage, ok := objField(info, "last_token_usage")
	if !ok {
		usage, ok = objField(info, "total_token_usage")
		if ok {
			fromTotal = true
		} else {
			usage, ok = objField(payload, "usage")
		}
	}
	if !ok {
		return core.UsageEvent{}, false
	}

	inRaw := nonNeg(usage.Raw("input_tokens"))
	if inRaw == 0 {
		inRaw = nonNeg(usage.Raw("inputTokens"))
	}
	outRaw := nonNeg(usage.Raw("output_tokens"))
	if outRaw == 0 {
		outRaw = nonNeg(usage.Raw("outputTokens"))
	}
	cRead := nonNeg(usage.Raw("cached_input_tokens"))
	if cRead == 0 {
		cRead = nonNeg(usage.Raw("cache_read_input_tokens"))
	}
	reasoning := nonNeg(usage.Raw("reasoning_output_tokens"))
	cWrite := nonNeg(usage.Raw("cache_write_input_tokens"))
	if cWrite == 0 {
		cWrite = nonNeg(usage.Raw("cache_creation_input_tokens"))
	}

	// Same accounting as the reference implementation: cache sits inside
	// input and reasoning inside output, so each is subtracted and added back,
	// leaving total = input_tokens + output_tokens + cache_creation.
	tok := token4{
		input:      max(0, inRaw-cRead),
		output:     max(0, outRaw-reasoning) + reasoning,
		cacheRead:  cRead,
		cacheWrite: cWrite,
	}
	tokens := tok.total()
	if tokens <= 0 {
		return core.UsageEvent{}, false
	}

	if fromTotal {
		now := nonNeg(usage.Raw("total_tokens"))
		if now <= 0 {
			now = tokens
		}
		prev := a.bestTotals[uuid]
		if now < prev {
			prev = 0 // the session was reset → recount from zero
		}
		tokens = now - prev
		if tokens <= 0 {
			return core.UsageEvent{}, false
		}
		a.bestTotals[uuid] = now
	}

	ts, ok := parseISO8601(stamp)
	if !ok {
		ts = time.Now()
	}

	idSuffix := stamp
	if !hasStamp {
		idSuffix = stableHash(line)
	}
	return core.UsageEvent{
		ID:        "every-code:" + uuid + ":" + idSuffix,
		Source:    core.EveryCode,
		Timestamp: ts,
		Tokens:    tokens,
		Breakdown: tok.breakdown(),
		FilePath:  filePath,
	}, true
}

// ---------------------------------------------------------------------------
// Deliberately not collected
//
//   Kiro / Antigravity / QwenWork
//     These tools' local logs carry no real token metadata; their official
//     trackers estimate it from character counts. CodingFire's fire is a
//     human-facing trend signal, and mixing in estimates only makes it noisy.
//
//   Mimo / Kilo CLI / Hermes / Goose / Zed / Warp
//     They need a SQLite database that is not present on this machine. The
//     reader exists; what is missing is confirming each database's table
//     layout, which is a matter of adding another opencodeStyleAdapter.
//
//   Trae
//     Its data sits in a SQLCipher-encrypted database, which would need a key
//     derivation implementation. Not carried.
//
//   Cursor
//     The macOS original pulled Cursor's numbers from an official dashboard
//     API; the local state.vscdb holds no token counts at all.
// ---------------------------------------------------------------------------
