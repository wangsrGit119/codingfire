package core

import (
	"math"
	"strconv"
	"strings"
)

// AppLanguage is the user's language preference. System follows the OS.
type AppLanguage int

const (
	LangSystem AppLanguage = iota
	LangEnglish
	LangChineseSimplified
	LangJapanese
	LangKorean
)

// Column indices into every row of the translation table.
const (
	colEN = iota
	colZH
	colJA
	colKO
)

// Table holds one row per key, in column order en / zh-Hans / ja / ko.
// Key names follow the macOS version's Localizable.xcstrings conventions.
var Table = map[string][4]string{
	// ---- app ----
	"app.name": {"CodingFire", "CodingFire", "CodingFire", "CodingFire"},
	"app.tagline": {
		"If you're burning tokens anyway, light a real fire.",
		"既然都在烧 token，不如真的生一把火。",
		"どうせトークンを燃やすなら、本物の火を灯そう。",
		"토큰을 태우는 김에, 진짜 불을 피워보세요."},

	// ---- tiers ----
	"tier.hush":    {"Hush", "微火", "微火", "잔불"},
	"tier.glow":    {"Glow", "小火", "小火", "작은 불"},
	"tier.crackle": {"Crackle", "中火", "中火", "중간 불"},
	"tier.roar":    {"Roar", "大火", "大火", "큰 불"},
	"tier.blaze":   {"Blaze", "烈火", "烈火", "맹렬한 불"},

	// ---- phases ----
	"phase.unlit": {"Unlit", "未点燃", "未点火", "미점화"},
	"phase.flame": {"Burning", "燃烧中", "燃焼中", "연소 중"},
	"phase.ember": {"Embers", "余烬", "残り火", "잔불"},
	"phase.out":   {"Out", "已熄灭", "消火", "꺼짐"},

	// ---- sizes ----
	"size.small":  {"Small", "小", "小", "작게"},
	"size.medium": {"Medium", "中", "中", "중간"},
	"size.large":  {"Large", "大", "大", "크게"},

	// ---- source states ----
	"source.state.ok":           {"Connected", "已连接", "接続済み", "연결됨"},
	"source.state.notFound":     {"Not found", "未找到", "見つかりません", "찾을 수 없음"},
	"source.state.noPermission": {"No permission", "无权限", "権限なし", "권한 없음"},
	"source.state.unsupported":  {"Unsupported", "不支持", "非対応", "지원 안 함"},
	"source.state.readError":    {"Read error", "读取失败", "読み取りエラー", "읽기 오류"},

	// ---- source display-name overrides (regional variants only) ----
	// WorkBuddy (CN) writes ~/.workbuddy, WorkBuddy (INTL) writes
	// ~/.workbuddy-ai. Two independent installs, so they need a region note
	// or two identical "WorkBuddy" rows are indistinguishable.
	"source.name.workbuddy":      {"WorkBuddy (CN)", "WorkBuddy（内）", "WorkBuddy (CN)", "WorkBuddy (CN)"},
	"source.name.workbuddy-intl": {"WorkBuddy (INTL)", "WorkBuddy（外）", "WorkBuddy (INTL)", "WorkBuddy (INTL)"},

	// ---- hover card ----
	"hover.today":    {"Today", "今日 Tokens", "本日", "오늘"},
	"hover.rate":     {"Rate", "速率", "レート", "속도"},
	"hover.tokensS":  {"tok/s", "token/秒", "tok/s", "tok/s"},
	"hover.updated":  {"Updated", "更新于", "更新", "업데이트"},
	"hover.estimate": {"estimated", "估算", "推定", "추정"},
	"hover.none": {"No supported local usage logs found yet.",
		"尚未发现支持的本地用量记录。",
		"対応するローカル利用ログが見つかりません。",
		"지원되는 로컬 사용 로그를 찾지 못했습니다."},

	// ---- tray menu ----
	"menu.show":          {"Show campfire", "显示篝火", "焚き火を表示", "모닥불 표시"},
	"menu.hideFlame":     {"Hide campfire", "隐藏篝火", "焚き火を隠す", "모닥불 숨기기"},
	"menu.console":       {"Console…", "打开控制台…", "コンソール…", "콘솔 열기…"},
	"menu.resetPosition": {"Reset position", "重置位置", "位置をリセット", "위치 초기화"},
	"menu.autoStart":     {"Start with Windows", "开机自启", "Windows 起動時に開始", "Windows 시작 시 실행"},
	"menu.size":          {"Campfire size", "火焰尺寸", "焚き火サイズ", "모닥불 크기"},
	"menu.language":      {"Language", "语言", "言語", "언어"},
	"menu.about":         {"About CodingFire", "关于 CodingFire", "CodingFire について", "CodingFire 정보"},
	"menu.project":       {"Project page", "项目主页", "プロジェクトページ", "프로젝트 페이지"},
	"menu.quit":          {"Quit", "退出", "終了", "종료"},
	// go-gui cannot pass clicks through transparent pixels the way
	// UpdateLayeredWindow did, so pass-through is a switch the user owns.
	"menu.clickThrough": {"Click-through", "鼠标穿透", "マウス透過", "마우스 통과"},

	// ---- console ----
	"console.title":        {"CodingFire Console", "CodingFire 控制台", "CodingFire コンソール", "CodingFire 콘솔"},
	"console.tab.stats":    {"Stats", "统计", "統計", "통계"},
	"console.tab.sources":  {"Sources", "数据源", "データ源", "데이터 소스"},
	"console.tab.settings": {"Settings", "设置", "設定", "설정"},
	"console.tab.about":    {"About", "关于", "情報", "정보"},

	"stats.today":         {"Today total", "今日总量", "本日の合計", "오늘 합계"},
	"stats.bySource":      {"By source", "分来源", "データ源別", "소스별"},
	"stats.breakdown":     {"Breakdown", "用量构成", "内訳", "구성"},
	"stats.timeline":      {"Today timeline", "今日时间线", "本日のタイムライン", "오늘 타임라인"},
	"stats.recent":        {"Recent records", "最近记录", "最近の記録", "최근 기록"},
	"stats.overview":      {"Usage overview", "用量概览", "使用量の概要", "사용량 개요"},
	"stats.last7":         {"Last 7 days", "近 7 天", "過去 7 日間", "최근 7일"},
	"stats.last30":        {"Last 30 days", "近 30 天", "過去 30 日間", "최근 30일"},
	"stats.activeDays":    {"Active days", "活跃天数", "アクティブ日数", "활성 일수"},
	"stats.storedHistory": {"Stored history", "已保存历史", "保存履歴", "저장된 기록"},
	"stats.tier":          {"Fire tier", "当前火势", "火力", "불 세기"},
	"stats.peak":          {"Peak hour", "峰值时段", "ピーク時間", "최고 시간대"},

	"breakdown.input":      {"Input", "输入", "入力", "입력"},
	"breakdown.output":     {"Output", "输出", "出力", "출력"},
	"breakdown.cacheRead":  {"Cache read", "缓存读取", "キャッシュ読", "캐시 읽기"},
	"breakdown.cacheWrite": {"Cache write", "缓存写入", "キャッシュ書", "캐시 쓰기"},

	"sources.rescan":   {"Rescan", "重新检测", "再スキャン", "다시 검사"},
	"sources.source":   {"Source", "来源", "データ源", "소스"},
	"sources.state":    {"State", "状态", "状態", "상태"},
	"sources.path":     {"Path", "路径", "パス", "경로"},
	"sources.today":    {"Today", "今日", "本日", "오늘"},
	"sources.lastRead": {"Last read", "最近读取", "最終読み取り", "마지막 읽기"},
	"sources.scanning": {"Scanning…", "扫描中…", "スキャン中…", "검사 중…"},

	"settings.flame":           {"Campfire", "篝火", "焚き火", "모닥불"},
	"settings.flameSize":       {"Size", "尺寸", "サイズ", "크기"},
	"settings.visible":         {"Show on desktop", "在桌面显示", "デスクトップに表示", "바탕 화면에 표시"},
	"settings.reduceMotion":    {"Reduce motion", "减少动态效果", "モーションを減らす", "모션 줄이기"},
	"settings.showRate":        {"Show live rate on hover", "悬停显示实时速率", "ホバー時にレート表示", "호버 시 속도 표시"},
	"settings.language":        {"Language", "语言", "言語", "언어"},
	"settings.colorSources":    {"Flame color", "火焰颜色", "炎の色", "불꽃 색상"},
	"settings.colorSourcesAll": {"All", "不限", "すべて", "전체"},
	"settings.lang.system":     {"System", "跟随系统", "システム", "시스템"},
	"settings.night":           {"Night", "夜间", "夜", "밤"},
	"settings.colors":          {"Flame color", "火焰颜色", "炎の色", "불꽃 색상"},
	"settings.colorsHint": {"Choose a preset or click ... to pick a custom color.",
		"选择预设色或点击 ... 自定义颜色。",
		"プリセットを選ぶか「...」でカスタム色を選択します。",
		"프리셋을 선택하거나 ...를 클릭하여 사용자 지정 색상을 선택합니다."},
	"settings.flameColor":     {"Flame Color", "火焰颜色", "炎の色", "불꽃 색상"},
	"settings.flameColorHint": {"Current color preview", "当前颜色预览", "現在の色プレビュー", "현재 색상 미리보기"},
	"settings.flameCustom":    {"Custom color...", "自定义颜色...", "カスタムカラー...", "사용자 지정 색..."},
	"settings.resetColors":    {"Reset colors", "恢复默认配色", "色をリセット", "색 초기화"},
	"settings.resetPosition":  {"Reset to bottom-right", "重置到右下角", "右下に戻す", "오른쪽 아래로 초기화"},
	"settings.preview":        {"Preview", "火势预览", "プレビュー", "미리보기"},
	"settings.live":           {"Live", "实时", "ライブ", "실시간"},
	"settings.clickThrough":   {"Click-through (ignore mouse)", "鼠标穿透（不拦截点击）", "マウス透過（クリックを無視）", "마우스 통과(클릭 무시)"},
	"settings.clickThroughHint": {"Off: the campfire takes clicks. On: clicks reach the desktop below; hold the left button over the campfire to drag it.",
		"关闭：篝火会接收点击。开启：点击会传给下面的桌面；按住篝火上的鼠标左键仍可拖动。",
		"オフ：焚き火がクリックを受け取ります。オン：クリックは下のデスクトップへ届き、焚き火上で左ボタンを押したままドラッグできます。",
		"끔: 모닥불이 클릭을 받습니다. 켬: 클릭이 아래 바탕화면으로 전달되며 모닥불 위에서 왼쪽 버튼을 누른 채 드래그할 수 있습니다."},
	"settings.autoStart": {"Start with Windows", "开机自启", "Windows 起動時に開始", "Windows 시작 시 실행"},
	"settings.position":  {"Position", "位置", "位置", "위치"},

	"about.privacy": {"CodingFire only reads local usage logs. Nothing is uploaded.",
		"CodingFire 只读取本机日志，用量数据不会上传。",
		"CodingFire はローカルのログのみを読み取ります。アップロードは行いません。",
		"CodingFire는 로컬 로그만 읽습니다. 어떤 데이터도 업로드하지 않습니다."},
	"about.origin": {"A Windows port of wdkwdkwdk/tinyfire (macOS, MIT), rebuilt in Go with go-gui.",
		"本项目是 wdkwdkwdk/tinyfire（macOS，MIT 协议）的 Windows 版本，用 Go + go-gui 重写。",
		"wdkwdkwdk/tinyfire（macOS, MIT）の Windows 版を Go + go-gui で再実装。",
		"wdkwdkwdk/tinyfire(macOS, MIT)의 Windows 포트로 Go + go-gui로 재작성했습니다."},
	"about.powered": {"Go 1.26+ with go-gui · no installer · runs on Windows 10 and later",
		"Go 1.26+ 与 go-gui · 免安装 · Windows 10 及以上可直接运行",
		"Go 1.26+ と go-gui · インストーラ不要 · Windows 10 以降で動作",
		"Go 1.26+ 및 go-gui · 설치 불필요 · Windows 10 이상에서 실행"},
	"about.sources": {"Aggregates 22 local tools: Claude Code, Codex, Grok, Pi, Amp, WorkBuddy, CodeBuddy, Qoder, Qwen Code, Kimi, GitHub Copilot, ZCode, OpenCode, Gemini CLI, Droid, Cline, Roo Code, Kilo Code, DeepSeek Harness, Command Code, OpenClaw and Every Code.",
		"共聚合 22 种本地工具：Claude Code、Codex、Grok、Pi、Amp、WorkBuddy、CodeBuddy、Qoder、Qwen Code、Kimi、GitHub Copilot、ZCode、OpenCode、Gemini CLI、Droid、Cline、Roo Code、Kilo Code、DeepSeek Harness、Command Code、OpenClaw、Every Code。",
		"22 種類のローカルツールを集約します: Claude Code, Codex, Grok, Pi, Amp, WorkBuddy, CodeBuddy, Qoder, Qwen Code, Kimi, GitHub Copilot, ZCode, OpenCode, Gemini CLI, Droid, Cline, Roo Code, Kilo Code, DeepSeek Harness, Command Code, OpenClaw, Every Code。",
		"22개 로컬 도구를 집계합니다: Claude Code, Codex, Grok, Pi, Amp, WorkBuddy, CodeBuddy, Qoder, Qwen Code, Kimi, GitHub Copilot, ZCode, OpenCode, Gemini CLI, Droid, Cline, Roo Code, Kilo Code, DeepSeek Harness, Command Code, OpenClaw, Every Code."},
	"about.gap": {"Deliberately not collected: Cursor (its usage only exists behind a Dashboard API) and the tools that expose nothing but a character-count estimate (Kiro, Antigravity, QwenWork). Estimates would turn the flame into noise.",
		"刻意不采集：Cursor（它的用量只存在于 Dashboard API 后面），以及只提供「按字符数估算」的工具（Kiro、Antigravity、QwenWork）——估出来的数字只会让火焰变成噪音。",
		"意図的に収集しません: Cursor（Dashboard API 経由のみ）と、文字数からの推定値しか出さないツール（Kiro, Antigravity, QwenWork）。推定値は炎をノイズに変えてしまいます。",
		"의도적으로 수집하지 않음: Cursor(Dashboard API 전용)와 문자 수 추정치만 제공하는 도구(Kiro, Antigravity, QwenWork). 추정치는 불꽃을 노이즈로 만듭니다."},

	// ---- units ----
	"unit.tokens": {"tokens", "tokens", "tokens", "tokens"},
	"tier.label":  {"Tier", "档位", "段階", "단계"},
}

// SystemLanguageName is a seam: it returns the OS UI language as a short tag
// such as "zh-CN" or "ja-JP". Overridable so --dump can pin English and tests
// stay deterministic.
var SystemLanguageName = platformLanguageName

var (
	current      = LangSystem
	resolved     = "en"
	resolvedOnce bool
)

// LanguageChanged listeners are called after a user-driven language switch.
// The first resolution is not a switch and does not fire them.
var LanguageChanged []func()

// CurrentLanguage returns the configured preference.
func CurrentLanguage() AppLanguage { return current }

// SetLanguage sets the preference and re-resolves it.
//
// The "skip when unchanged" short-circuit has a trap: current starts as
// LangSystem, which is also Settings.Language's default. So the startup call
//
//	core.SetLanguage(settings.Language)
//
// would be short-circuited, Resolve would never run, and resolved would stay
// at its "en" initial value — the bug shows up as "language is set to Follow
// system, but a Chinese system still displays English". Only skip when the
// value is unchanged AND we have already resolved once.
func SetLanguage(l AppLanguage) {
	if current == l && resolvedOnce {
		return
	}
	// The first resolution is not a user switch and must not broadcast —
	// there are usually no listeners yet anyway.
	changed := resolvedOnce && current != l
	current = l
	resolved = resolve(l)
	resolvedOnce = true

	if !changed {
		return
	}
	for _, fn := range LanguageChanged {
		fn()
	}
}

// ResolvedCode returns the active language column tag ("en", "zh-Hans", ...).
func ResolvedCode() string { return resolved }

func resolve(l AppLanguage) string {
	switch l {
	case LangEnglish:
		return "en"
	case LangChineseSimplified:
		return "zh-Hans"
	case LangJapanese:
		return "ja"
	case LangKorean:
		return "ko"
	default:
		// There is no zh-Hant column: zh-TW falls back to zh-Hans on purpose.
		two := strings.ToLower(SystemLanguageName())
		if i := strings.IndexAny(two, "-_"); i >= 0 {
			two = two[:i]
		}
		switch two {
		case "zh":
			return "zh-Hans"
		case "ja":
			return "ja"
		case "ko":
			return "ko"
		default:
			return "en"
		}
	}
}

func column() int {
	switch resolved {
	case "zh-Hans":
		return colZH
	case "ja":
		return colJA
	case "ko":
		return colKO
	default:
		return colEN
	}
}

// T returns the localised string for key.
//
// A missing key returns the key itself, so a typo shows up on screen as a raw
// "menu.something" rather than an empty label — and it cannot be caught at
// compile time. After adding keys, run the app once or probe Table directly.
func T(key string) string {
	row, ok := Table[key]
	if !ok {
		return key
	}
	return row[column()]
}

// L10nHas reports whether a key is defined. Used for "use the override when
// present, else fall back to the static English name".
func L10nHas(key string) bool {
	if key == "" {
		return false
	}
	_, ok := Table[key]
	return ok
}

// Compact renders 1200 as "1.2k", 3400000 as "3.4M".
func Compact(value int64) string {
	units := []struct {
		divisor float64
		suffix  string
	}{
		{1e9, "B"},
		{1e6, "M"},
		{1e3, "k"},
	}
	for i, unit := range units {
		if absF(float64(value)) < unit.divisor {
			continue
		}
		v := float64(value) / unit.divisor
		precision := 2
		if absF(v) >= 100 {
			precision = 0
		} else if absF(v) >= 10 {
			precision = 1
		}
		factor := math.Pow10(precision)
		rounded := math.Round(v*factor) / factor
		// Let 999.999k display as 1.00M rather than the awkward "1000k".
		if absF(rounded) >= 1000 && i > 0 {
			next := units[i-1]
			nextV := float64(value) / next.divisor
			nextPrecision := 2
			if absF(nextV) >= 100 {
				nextPrecision = 0
			} else if absF(nextV) >= 10 {
				nextPrecision = 1
			}
			nextFactor := math.Pow10(nextPrecision)
			nextRounded := math.Round(nextV*nextFactor) / nextFactor
			return strconv.FormatFloat(nextRounded, 'f', nextPrecision, 64) + next.suffix
		}
		return strconv.FormatFloat(rounded, 'f', precision, 64) + unit.suffix
	}
	if value >= -999 && value <= 999 {
		return strconv.FormatInt(value, 10)
	}
	return strconv.FormatInt(value, 10)
}

// Grouped renders 1234567 as "1,234,567".
func Grouped(value int64) string {
	s := strconv.FormatInt(value, 10)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

func absF(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
