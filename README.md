# CodingFire

Turn your AI coding token burn into a pixel campfire on the desktop. The faster
you burn tokens, the bigger the fire.

[![Release](https://img.shields.io/github/v/release/wangsrGit119/codingfire)](../../releases)
[![Platform](https://img.shields.io/badge/platform-Windows%20%7C%20Linux%20%7C%20macOS-lightgrey)](#requirements)
[![CI](https://github.com/wangsrGit119/codingfire/actions/workflows/ci.yml/badge.svg)](../../actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](./LICENSE)

**English** · [简体中文](README.zh-CN.md)

<p align="center">
  <img src="assets/example_01.gif" width="344" alt="CodingFire - campfire with a green-tinted flame">
  <img src="assets/example_02.gif" width="344" alt="CodingFire - campfire with the classic orange flame">
</p>

## Features

- **Fire intensity is your current token burn rate.** Several AI tools running at
  once still share one fire - their rates are summed.
- **Hover the flame** for today's usage, a live tok/s reading, and the current tier.
- **Right-click or double-click the tray icon** for the console: Stats (today, an
  hourly timeline, the per-source breakdown, the peak rate), Sources, Settings, About.
- **Always on top, click-through by default**, so it never swallows a click meant
  for the desktop underneath. Hold the left button over it to drag it.
- **Starts with the desktop**, unless you turn that off.
- **Four UI languages** - English, 简体中文, 日本語, 한국어 - or follow the system.
- **Read-only and offline.** No network, no uploads, no telemetry; prompts, code
  and file contents are never read.
- **One static binary.** No .NET, no DLLs, no installer, no admin rights.

## Requirements

| OS | Needs |
|---|---|
| Windows 11 / 10 (64-bit) | Nothing |
| Linux (x64, arm64) | An X11 session with a compositing manager; a StatusNotifier host for the tray |
| macOS (Intel, Apple silicon) | Nothing beyond Gatekeeper's approval |

Download the zip for your platform from [Releases](../../releases), unpack it and
run it - each zip holds exactly one file. With no data yet the fire stays in an
"embers" state, which is normal.

Windows 7, Windows 8 and 32-bit Windows: use the
[C# build](https://github.com/wangsrGit119/codingfire-win).

A few platform notes. Linux draws its transparent window through the compositor -
without one you get an opaque rectangle, and without a StatusNotifier host (GNOME
needs an extension) there is no menu to quit from. macOS is unsigned, so Gatekeeper
blocks the first launch until you allow it under System Settings › Privacy &
Security; the app lives in the menu bar and has no Dock icon. Per-pixel
click-through is not implemented on any platform: go-gui renders a window as one
surface, so pass-through is all-or-nothing. Windows is the most exercised target,
and `windows/arm64` is built but has never been run.

## Data sources

**23 read-only local sources (22 tools).** Nothing is uploaded, and no prompt or
source file is ever read - only token counters and file paths.

- Claude Code, Codex, Grok, Pi, Amp
- WorkBuddy, CodeBuddy, Qoder, Qwen Code, Kimi, GitHub Copilot CLI, ZCode, OpenCode,
  Gemini CLI, Droid, Cline / Roo Code / Kilo Code, DeepSeek Harness, Command Code,
  OpenClaw, Every Code

WorkBuddy's China and international builds are separate installations with separate
home directories, so they are counted separately - hence 23 sources but 22 tools.
Every source accepts an environment variable to override its log root
(`CLAUDE_CONFIG_DIR`, `CODEX_HOME`, `WORKBUDDY_HOME`, ...); see
`internal/data/adapters*.go`.

Only billable tokens are counted, cache reads and writes stay separate columns, and
`reasoning` is not double-counted. Cumulative sources use a high-water mark per
event id, so restarts never double-count. Deliberately not collected: Cursor, Kiro,
Antigravity, QwenWork, Trae, and a few tools whose log schema could not be verified.

## Build

Requires **Go 1.26 or later**. Windows needs no C compiler; macOS needs Xcode's
command line tools, because the Metal backend is cgo.

```bash
CGO_ENABLED=0 go build -trimpath -ldflags '-s -w' -o dist/CodingFire .   # Windows, Linux
CGO_ENABLED=1 go build -trimpath -ldflags '-s -w' -o dist/CodingFire .   # macOS
go vet ./... && go test ./...
```

On Windows, `build.ps1` wraps that up (`-Run`, `-Dump`, `-Render`), and
`release.ps1 -Version 1.1.0` zips and tags. `CGO_ENABLED=0` on macOS compiles and
then panics on launch with *"no native backend available"* - go-gui only selects
its Metal backend under cgo, which is why the macOS artifacts are built on a macOS
runner.

The version lives in one place, `internal/core/version.go`; the release workflow
reads it out of the source and refuses to publish if it does not match the tag. CI
builds and tests all three platforms on every push, and starts the app under Xvfb
on Linux, where the X11 window layer has no compile-time signature. One test is
opt-in - the overlay memory probe, `CODINGFIRE_MEMORY_PROBE=1 go test ./internal/ui
-run TestOverlayMemoryProbe`.

`third_party/go-gui` and `third_party/go-glyph` are patched forks wired in with
`replace` directives, committed on purpose so a clone builds offline. The go-gui
patches fix a Windows tray menu that shifts every action by one, and publish
`_NET_WM_PID` on X11, without which a window cannot be told apart from any other
client's. The go-glyph patch releases its font cache when the console closes.
**Re-check each after an upstream bump.**

## Releasing

Push a tag and [`.github/workflows/release.yml`](.github/workflows/release.yml)
builds, packages and publishes it:

```bash
git tag -a v1.1.0 -m "CodingFire v1.1.0"
git push origin v1.1.0
```

It aborts if `version.go` declares a different version, and publishes nothing
unless every platform built - each on a runner that matches it, each artifact
checked for the `GOOS`/`GOARCH` recorded inside it. Targets: `windows`, `linux` and
`darwin`, amd64 and arm64, one zip each. `release.ps1` does the Windows half
locally.

## Data and privacy

| OS | Directory |
|---|---|
| Windows | `%APPDATA%\CodingFireGo\` |
| Linux | `$XDG_CONFIG_HOME/CodingFireGo/` (usually `~/.config/CodingFireGo/`) |
| macOS | `~/Library/Application Support/CodingFireGo/` |

`usage.ndjson` is the event store (45-day retention), `cursors.json` the per-file
read offsets, `settings.json` your preferences, and `codingfire.log` errors only.
Set `CODINGFIRE_DATA_DIR` for a portable data directory instead.

The only thing written outside that folder is the autostart entry, and only while
the toggle is on. It needs no admin rights and is removed again the moment you turn
autostart off: an `HKCU\...\Run` value on Windows, an XDG `.desktop` file on Linux,
a LaunchAgent on macOS. Every name carries the `Go` marker, so it cannot collide
with another copy of the app.

Headless self-checks:

```bash
CodingFire --dump report.txt   # plain-text statistics report, always in English
CodingFire --render out-dir    # render each fire tier to PNG
```

## Resource usage

- The fallback log scan runs every **4 seconds**; file-change notifications trigger
  it sooner, and unchanged files with a valid cursor are skipped before any read
  buffer is allocated.
- The flame runs at **10 fps**, embers at **4 fps**; hidden flames are not redrawn.
  State and mouse polling stay at 20 Hz.
- On Windows the flame and hover card use reusable DIBs and the console uses
  software drawing, so normal operation creates no OpenGL context. Off Windows
  everything is drawn by go-gui's OpenGL backend, so a GL driver is required -
  Mesa's `llvmpipe` works, and is what CI uses.
- Measured working set: about 33 MiB with the flame, 34 MiB with the hover card,
  37 MiB after closing the console. See
  [the full comparison](perf-artifacts/memory-optimization-report.md).

`CODINGFIRE_OVERLAY_BACKEND=gl` selects the older OpenGL path on Windows; leave it
unset for the default lower-memory backend. To cost even less, pick the small flame
size or hide the flame - token collection continues either way.

## Credits

A rewrite of the macOS app **[TinyFire](https://github.com/wdkwdkwdk/tinyfire)** by
[@wdkwdkwdk](https://github.com/wdkwdkwdk) (MIT), ported to Windows, Linux and
macOS. Data-source semantics are aligned with
[juejin-cn/juejin-usage](https://github.com/juejin-cn/juejin-usage). The desktop
window, tray and console are built with
[go-gui](https://github.com/go-gui-org/go-gui).

## License

MIT - see [LICENSE](./LICENSE).
