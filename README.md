# CodingFire for Windows (Go)

Turn your AI coding token burn into a pixel campfire on the desktop.

[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](./LICENSE)
[![Platform](https://img.shields.io/badge/platform-Windows%2010%20%7C%2011-lightgrey)](#requirements)
[![Go](https://img.shields.io/badge/Go-1.26%2B-00ADD8)](#build-from-source)
[![Release](https://img.shields.io/github/v/release/wangsrGit119/codingfire)](../../releases)
[![CI](https://github.com/wangsrGit119/codingfire/actions/workflows/ci.yml/badge.svg)](../../actions/workflows/ci.yml)
[![Release workflow](https://github.com/wangsrGit119/codingfire/actions/workflows/release.yml/badge.svg)](../../actions/workflows/release.yml)

**English** · [简体中文](README.zh-CN.md)

<p align="center">
  <img src="assets/example_01.gif" width="344" alt="CodingFire - campfire with a green-tinted flame">
  <img src="assets/example_02.gif" width="344" alt="CodingFire - campfire with the classic orange flame">
</p>

A small always-on-top campfire that reads the token usage logs your AI coding
tools already write to disk. The faster you burn tokens, the bigger the fire.

- **Fire intensity = current token burn rate.** Several clients at once still
  share a single fire; their rates are summed
- **Hover for today's usage card** with a live tok/s reading and the current tier
- **Read-only.** No network, no uploads, and prompts or code are never read
- **Zero runtime dependencies** - one static `.exe`, no .NET, no installer

> This is the Go + [go-gui](https://github.com/go-gui-org/go-gui) rewrite. It is
> feature-for-feature with the
> [C#/.NET build](https://github.com/wangsrGit119/codingfire-win), which is still
> the one to use on Windows 7 and 8. **The two install side by side** - separate
> data directories, separate autostart entries, separate single-instance locks.

## Requirements

| OS | Needs |
|---|---|
| Windows 11 / 10 (64-bit) | Nothing - one self-contained `.exe` |
| Windows 8 / 8.1, Windows 7 SP1 | Use the [C# build](https://github.com/wangsrGit119/codingfire-win) instead |

Go 1.21 dropped Windows 7 and 8 support, so this build is Windows 10 and later.
It is compiled for `windows/amd64` with `CGO_ENABLED=0` and links no C runtime:
the binary is fully static, which is why it is ~18 MB rather than the C# build's
191 KB. Disk is cheap; a missing DLL is not.

## Download and run

Grab the latest zip from [Releases](../../releases), unpack it anywhere and run
`CodingFire.exe`. There is nothing to install - the zip contains exactly one file.

- **Hover the fire** - today's usage card, live tok/s, current tier
- **Right-click / double-click the tray icon** - menu and statistics console
- **Starts with Windows by default** - turn it off any time from the tray menu

With no data yet the fire stays in an "embers" state. That is normal.

### Click-through

The Windows flame and hover card use native layered bitmap windows, like the
C# build. Transparent pixels pass clicks through. This build also provides a
**Click-through** toggle (tray menu, and the
Settings tab of the console), **on by default**, so the campfire does not swallow
clicks meant for the desktop icons underneath it. Hold the left mouse button over
the campfire to drag it; the app polls the global button state because a
click-through window does not receive normal mouse messages. Turn click-through
off if you want the campfire window itself to receive clicks.

## Supported data sources

**23 read-only local sources (22 tools).** Nothing is uploaded, and no prompt or
source file is ever read - only token counters and file paths.

- **From the original macOS app** - Claude Code, Codex, Grok, Pi, Amp
- **Aligned with [juejin-cn/juejin-usage](https://github.com/juejin-cn/juejin-usage)** -
  WorkBuddy, CodeBuddy, Qoder, Qwen Code, Kimi, GitHub Copilot CLI, ZCode,
  OpenCode, Gemini CLI, Droid, Cline / Roo Code / Kilo Code, DeepSeek Harness,
  Command Code, OpenClaw, Every Code

WorkBuddy's China and international builds are separate installations with
separate home directories (`~/.workbuddy` and `~/.workbuddy-ai`), so they are
counted separately and shown as `WorkBuddy (CN)` / `WorkBuddy (INTL)`. That is
why the count is 23 sources but 22 tools.

Every source accepts an environment variable to override its log root
(`CLAUDE_CONFIG_DIR`, `CODEX_HOME`, `WORKBUDDY_HOME`, `WORKBUDDY_AI_HOME`, ...).
See `internal/data/adapters*.go`.

**Counting rules.** Only billable tokens are counted; cache reads and writes are
kept as separate columns and `reasoning` is not double-counted. Cumulative
sources are tracked by high-water mark per event id, so restarts never
double-count. ZCode subagent messages are attributed to their own source.

**Deliberately not collected.** Cursor (usage only exists behind its cloud API),
Kiro / Antigravity / QwenWork (no real token metadata locally - the reference
implementations estimate from character counts), Trae (SQLCipher encrypted), and
a few tools whose log schema could not be verified.

## Build from source

Requires **Go 1.26 or later**. No C compiler, no Visual Studio.

```powershell
powershell -ExecutionPolicy Bypass -File build.ps1            # vet + build -> dist\
powershell -ExecutionPolicy Bypass -File build.ps1 -Run       # build, then launch
powershell -ExecutionPolicy Bypass -File build.ps1 -Dump      # build, then write a usage report
powershell -ExecutionPolicy Bypass -File build.ps1 -Render    # build, then render the fire tiers
powershell -ExecutionPolicy Bypass -File release.ps1 -Version 1.0.0   # zip, tag, release
```

The build runs `go vet ./...` before compiling and refuses to continue if it
reports anything. The output is `dist\CodingFire.exe`, built with `-trimpath` and
`-ldflags "-s -w -H=windowsgui"` so it opens without a console window.

The version lives in exactly one place, `internal/core/version.go`. A Go binary
carries no version resource, so `release.ps1` and the release workflow both read
that constant out of the source and refuse to publish if it does not match the
tag.

### Tests

```powershell
go test ./...    # all packages
go vet ./...     # what build.ps1 runs before it compiles
```

CI runs both on every push and pull request
([`.github/workflows/ci.yml`](.github/workflows/ci.yml)).

One test is opt-in: the overlay memory probe, which opens real windows and takes
about 16 seconds. It is skipped unless you ask for it:

```powershell
$env:CODINGFIRE_MEMORY_PROBE = '1'
go test ./internal/ui -run TestOverlayMemoryProbe -v -count=1
```

### Vendored dependencies

`third_party/go-gui` and `third_party/go-glyph` are patched forks, wired in with
`replace` directives in `go.mod`. They are committed on purpose, so a clone
builds without fetching anything:

- **go-gui** - on Windows the tray menu does not skip its root sentinel node,
  which shifts every menu action by one, and submenus do not return their index
  to the sibling items that follow. The fork fixes both. Re-check this patch
  after any upstream bump.
- **go-glyph** - upstream keeps a global font cache of up to 384 MiB and only
  evicts entries after five idle minutes, which is the wrong trade for a small
  always-on desktop tool. The fork adds a release call so closing the console
  hands the memory back.

### Releasing

Releases are cut by CI. Push a tag and
[`.github/workflows/release.yml`](.github/workflows/release.yml) builds, packages
and publishes it:

```bash
git tag -a v1.0.0 -m "CodingFire for Windows (Go) v1.0.0"
git push origin v1.0.0
```

It aborts if `internal/core/version.go` declares a different version than the
tag. You can also run it by hand from the Actions tab and type the version in.

`release.ps1` does the same work locally, for when you would rather not wait for
a runner.

## Data and privacy

Everything lives in `%APPDATA%\CodingFireGo\`:

| File | Purpose |
|---|---|
| `usage.ndjson` | Event store, 45-day retention |
| `cursors.json` | Per-file read offsets |
| `settings.json` | Size, position, language, colors, autostart |
| `codingfire.log` | Errors only |

Set `CODINGFIRE_DATA_DIR` to use a portable data directory instead.

The one thing written outside that folder is a single
`HKCU\Software\Microsoft\Windows\CurrentVersion\Run` entry for the autostart
toggle, under the value name `CodingFireGo`. It needs no admin rights, and it is
deleted again the moment you turn autostart off. The C# build uses the value name
`CodingFire`, so the two do not collide.

Headless self-checks:

```powershell
CodingFire.exe --dump report.txt   # statistics report
CodingFire.exe --render out-dir    # render each fire tier to PNG
```

`--dump`'s format is byte-compatible with the C# build's, so a report from either
can be compared directly. The report always uses English, regardless of your
configured UI language.

## Resource usage

The fallback log scan runs every **4 seconds**, alongside file-change
notifications. Unchanged JSONL files with a valid cursor are skipped before
allocating read buffers. History is decoded line by line at startup.

The flame runs at **10 fps**, embers at **4 fps**. Hidden flames receive no
scheduled redraws; extinguished flames draw once when the phase changes.
The state machine and mouse polling remain at 20 Hz. On Windows, the flame and
hover card use reusable DIBs and native text. The console uses software drawing
and GDI presentation; normal operation creates no OpenGL context. Closing the
console destroys its window and releases the shared parsed-font cache. Hover
statistics refresh twice per second.

The memory probe measured approximately 33 MiB with the flame, 34 MiB with the
hover card, and 37 MiB after closing the console. Opening the console still has
a larger transient footprint (about 182 MiB in that probe). These are process
working-set measurements, not a fixed memory guarantee. See
[the full comparison](perf-artifacts/memory-optimization-report.md).

For rendering diagnostics, `CODINGFIRE_OVERLAY_BACKEND=gl` selects the previous
OpenGL path. Leave it unset for the default lower-memory Windows backend.

For lower rendering cost, select the small flame size or hide the flame from
the tray. Token collection continues while hidden. After rebuilding, quit the
old running copy and launch `dist\CodingFire.exe` to use the new executable.

## Credits

A Windows rewrite of the macOS app **[TinyFire](https://github.com/wdkwdkwdk/tinyfire)**
by [@wdkwdkwdk](https://github.com/wdkwdkwdk) (MIT). Thanks for the original idea
and the core algorithms. Data-source semantics are aligned with
[juejin-cn/juejin-usage](https://github.com/juejin-cn/juejin-usage). The desktop
window, tray and console are built with
[go-gui](https://github.com/go-gui-org/go-gui).

## License

MIT - see [LICENSE](./LICENSE).
