# CodingFire (Go)

Turn your AI coding token burn into a pixel campfire on the desktop.

[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](./LICENSE)
[![Platform](https://img.shields.io/badge/platform-Windows%20%7C%20Linux%20%7C%20macOS-lightgrey)](#requirements)
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
| Linux (x64, arm64) | An X11 session with a compositing manager, and a StatusNotifier host for the tray |
| macOS (Intel, Apple silicon) | Nothing beyond Gatekeeper's approval - one unsigned binary |

Go 1.21 dropped Windows 7 and 8 support, so the Windows build is Windows 10 and
later. It is compiled with `CGO_ENABLED=0` and links no C runtime: the binary is
fully static, which is why it is ~18 MB rather than the C# build's 191 KB. Disk
is cheap; a missing DLL is not.

**Windows is the most thoroughly exercised target** - it is the one the app was
written for, and the only one whose overlay uses native layered bitmap windows.
Linux and macOS run through go-gui's X11 and Metal backends respectively, with a
platform shim of their own for the window management go-gui does not expose; see
[Platform notes](#platform-notes) for what each one needs and what is known to be
rough. `windows/arm64` is built and published but has never been run.

## Download and run

Grab the latest zip from [Releases](../../releases), unpack it anywhere and run
it. There is nothing to install - each zip contains exactly one file.

- **Hover the fire** - today's usage card, live tok/s, current tier
- **Right-click / double-click the tray icon** - menu and statistics console
- **Starts with the desktop by default** - turn it off any time from the menu

With no data yet the fire stays in an "embers" state. That is normal.

### Click-through

On Windows the flame and hover card use native layered bitmap windows, like the
C# build, so transparent pixels pass clicks through. Every platform also offers a
**Click-through** toggle (tray menu, and the Settings tab of the console), **on by
default**, so the campfire does not swallow clicks meant for the desktop icons
underneath it. Hold the left mouse button over the campfire to drag it; the app
polls the global button state because a click-through window does not receive
normal mouse messages. Turn click-through off if you want the campfire window
itself to receive clicks.

## Platform notes

The overlay is not one thing but three, one per platform, behind the same small
interface in `internal/ui/win32_*.go`. All three find their own window, keep it on
top, make it click-through and move it; none of them can be checked by a compiler.

| | Windows | Linux | macOS |
|---|---|---|---|
| Backend | Win32 + WGL | X11 + EGL | AppKit + Metal |
| On top | `SetWindowPos(HWND_TOPMOST)` | `_NET_WM_STATE_ABOVE` | `setLevel:` |
| Click-through | `WS_EX_TRANSPARENT` | empty `ShapeInput` region | `setIgnoresMouseEvents:` |
| Autostart | `HKCU\...\Run` | XDG `.desktop` | LaunchAgent plist |
| Tray | `Shell_NotifyIcon` | StatusNotifierItem (D-Bus) | `NSStatusItem` |

- **Linux** needs a compositing manager for the transparent window: without one
  the campfire is drawn on an opaque rectangle. The tray is a StatusNotifierItem,
  which KDE, XFCE and LXQt all host and GNOME only hosts with an extension
  installed - without a host the app still runs, but has no menu to quit from.
- **macOS** ships unsigned, so Gatekeeper blocks the first launch until it is
  allowed under System Settings › Privacy & Security. The app has no Dock icon
  and lives in the menu bar.
- **Not yet implemented anywhere:** per-pixel click-through. go-gui renders a
  window as a single surface and hit-tests its own widgets, so pass-through is
  all-or-nothing. The C# build gets per-pixel pass-through from
  `UpdateLayeredWindow`; this one cannot.

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

Requires **Go 1.26 or later**. On Windows, no C compiler and no Visual Studio.
On macOS a C toolchain comes with Xcode's command line tools, and it is required:
the Metal backend is cgo.

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

Off Windows, `go build` is all there is - `build.ps1` and `release.ps1` are
Windows scripts:

```bash
# Linux: pure Go, no cgo, cross-compiles from anywhere
CGO_ENABLED=0 go build -trimpath -ldflags '-s -w' -o dist/CodingFire .

# macOS: cgo is not optional, and the build needs the macOS SDK
CGO_ENABLED=1 go build -trimpath -ldflags '-s -w' -o dist/CodingFire .
```

`CGO_ENABLED=0` on macOS produces a binary that compiles and then panics on
launch with *"no native backend available"*: go-gui only selects its Metal
backend under cgo. This is why the macOS artifacts are built on a macOS runner
rather than cross-compiled.

The version lives in exactly one place, `internal/core/version.go`. A Go binary
carries no version resource, so `release.ps1` and the release workflow both read
that constant out of the source and refuse to publish if it does not match the
tag.

### Tests

```powershell
go test ./...    # all packages
go vet ./...     # what build.ps1 runs before it compiles
```

CI runs both on every push and pull request, on Windows, Linux and macOS
([`.github/workflows/ci.yml`](.github/workflows/ci.yml)). The Linux job also
*starts the app* under a virtual X server and checks that it came up
([`scripts/linux-smoke.sh`](scripts/linux-smoke.sh)) - the X11 window layer has
no compile-time signature, and getting it wrong is silent.

One test is opt-in: the overlay memory probe, which opens real windows and takes
about 16 seconds. It is Windows-only. It is skipped unless you ask for it:

```powershell
$env:CODINGFIRE_MEMORY_PROBE = '1'
go test ./internal/ui -run TestOverlayMemoryProbe -v -count=1
```

### Vendored dependencies

`third_party/go-gui` and `third_party/go-glyph` are patched forks, wired in with
`replace` directives in `go.mod`. They are committed on purpose, so a clone
builds without fetching anything:

- **go-gui** - three patches, each re-check after any upstream bump:
  - Windows: the tray menu does not skip its root sentinel node, which shifts
    every menu action by one, and submenus do not return their index to the
    sibling items that follow. The fork fixes both.
  - X11: `_NET_WM_PID` is not published when a window is created. X core has no
    ownership query, so without that property there is nothing to distinguish
    our windows from any other client's - and finding its own window is exactly
    what the overlay needs, since go-gui keeps the XID private.
- **go-glyph** - upstream keeps a global font cache of up to 384 MiB and only
  evicts entries after five idle minutes, which is the wrong trade for a small
  always-on desktop tool. The fork adds a release call so closing the console
  hands the memory back.

### Releasing

Releases are cut by CI. Push a tag and
[`.github/workflows/release.yml`](.github/workflows/release.yml) builds, packages
and publishes it:

```bash
git tag -a v1.0.0 -m "CodingFire (Go) v1.0.0"
git push origin v1.0.0
```

It aborts if `internal/core/version.go` declares a different version than the
tag. You can also run it by hand from the Actions tab and type the version in.

The workflow verifies once, then builds every target in parallel, then publishes -
so a platform that stops compiling fails the run *before* anything is attached to
the release. Targets: `windows/amd64`, `windows/arm64`, `linux/amd64`,
`linux/arm64`, `darwin/amd64`, `darwin/arm64`, each as a one-file zip.

Each target is built on a runner that matches it, which is what makes the
verification real rather than a compile check:

- **windows** and **linux** legs cross-compile with `CGO_ENABLED=0`. The Windows
  `amd64` binary is then run and its `--dump` header read back; the Linux `amd64`
  binary is run headlessly and then under Xvfb.
- **macos** legs build on a macOS runner with `CGO_ENABLED=1`, and the binary is
  run with `--dump`.
- Every artifact is also inspected with `go version -m` to confirm the recorded
  `GOOS`/`GOARCH` matches the name it ships under, plus a scan for the version
  literal.

`release.ps1` does the same work locally, for when you would rather not wait for
a runner. It only builds the Windows target.

## Data and privacy

Everything lives in one directory:

| OS | Directory |
|---|---|
| Windows | `%APPDATA%\CodingFireGo\` |
| Linux | `$XDG_CONFIG_HOME/CodingFireGo/` (usually `~/.config/CodingFireGo/`) |
| macOS | `~/Library/Application Support/CodingFireGo/` |

| File | Purpose |
|---|---|
| `usage.ndjson` | Event store, 45-day retention |
| `cursors.json` | Per-file read offsets |
| `settings.json` | Size, position, language, colors, autostart |
| `codingfire.log` | Errors only |

Set `CODINGFIRE_DATA_DIR` to use a portable data directory instead.

The one thing written outside that folder is the autostart entry, and only when
the autostart toggle is on. It needs no admin rights, and it is deleted again the
moment you turn autostart off:

| OS | Entry |
|---|---|
| Windows | `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, value `CodingFireGo` |
| Linux | `$XDG_CONFIG_HOME/autostart/CodingFireGo.desktop` |
| macOS | `~/Library/LaunchAgents/com.codingfire.go.plist` |

Every name carries the `Go` marker so it cannot collide with the C# build's,
which uses the unmarked names: the two are meant to install side by side, and a
shared name would make each silently disable the other's login entry.

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

Off Windows everything is drawn by go-gui's OpenGL backend instead, so Linux and
macOS need a working GL driver - a GPU, or Mesa's software rasteriser
(`llvmpipe`), which is what CI uses.

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

A rewrite of the macOS app **[TinyFire](https://github.com/wdkwdkwdk/tinyfire)**
by [@wdkwdkwdk](https://github.com/wdkwdkwdk) (MIT), aimed at Windows first and
since ported to Linux and macOS. Thanks for the original idea
and the core algorithms. Data-source semantics are aligned with
[juejin-cn/juejin-usage](https://github.com/juejin-cn/juejin-usage). The desktop
window, tray and console are built with
[go-gui](https://github.com/go-gui-org/go-gui).

## License

MIT - see [LICENSE](./LICENSE).
