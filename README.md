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
- **Light on resources.** A hidden flame is not redrawn and the fallback scan runs
  every 4 seconds - about 33 MiB of working set.
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

Per-pixel click-through is not implemented on any platform - go-gui renders a
window as one surface, so pass-through is all-or-nothing. Linux without a
compositor draws an opaque rectangle; macOS is unsigned, so Gatekeeper blocks the
first launch until you allow it. `windows/arm64` is built but has never been run.

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

## Data directory

| OS | Directory |
|---|---|
| Windows | `%APPDATA%\CodingFire\` |
| Linux | `$XDG_CONFIG_HOME/CodingFire/` (usually `~/.config/CodingFire/`) |
| macOS | `~/Library/Application Support/CodingFire/` |

This is the same directory the
[C# build](https://github.com/wangsrGit119/codingfire-win) uses, so the two are
interchangeable and only one of them can run at a time. Set `CODINGFIRE_DATA_DIR`
for a portable directory instead. Nothing outside it is written, except the startup
entry while that toggle is on.

`CodingFire --dump report.txt` writes the same statistics as plain text.

## Build

Requires **Go 1.26 or later**. Windows needs no C compiler; macOS needs Xcode's
command line tools, because the Metal backend is cgo.

```bash
CGO_ENABLED=0 go build -trimpath -ldflags '-s -w' -o dist/CodingFire .   # Windows, Linux
scripts/macos/build.sh                                              # macOS app bundle (dist/CodingFire.app)
go vet ./... && go test ./...
```

`scripts/` holds a per-platform wrapper around those commands, which is what you
normally want to run - each one pins the flags, vets first, and reports the version
the source claims:

```bash
scripts/macos/build.sh                 # build, plus --run / --dump / --render
scripts/macos/build.sh --check         # build, then prove it starts (headless)
scripts/macos/build.sh --universal     # one fat arm64 + x86_64 binary
```

The version lives in one place, `internal/core/version.go`. Releasing is entirely
[CI](../../actions/workflows/release.yml)'s job, and there is no local release script
on purpose — it would build one target where CI builds six, and race the run it just
triggered. To cut a release:

```bash
# bump the constant in internal/core/version.go, commit it, then:
git tag -a v1.0.4 -m "CodingFire v1.0.4"
git push origin v1.0.4
```

CI verifies `version.go` against the tag, builds and tests all six targets, checks
each binary reports the right version, and publishes nothing unless every platform
built. A mismatch fails the run before anything is published — fix the constant,
delete the tag, and push it again.

Locally, `scripts/windows/build.ps1` does the Windows build and
`scripts/linux/linux-smoke.sh` is what CI runs to exercise the X11 window layer under
a virtual display.

`third_party/go-gui` and `third_party/go-glyph` are patched forks wired in with
`replace` directives, committed on purpose so a clone builds offline. **Re-check each
after an upstream bump.**

## Credits

A rewrite of the macOS app **[TinyFire](https://github.com/wdkwdkwdk/tinyfire)** by
[@wdkwdkwdk](https://github.com/wdkwdkwdk) (MIT), ported to Windows, Linux and
macOS. Data-source semantics are aligned with
[juejin-cn/juejin-usage](https://github.com/juejin-cn/juejin-usage). The desktop
window, tray and console are built with
[go-gui](https://github.com/go-gui-org/go-gui).

## License

MIT - see [LICENSE](./LICENSE).
