#!/usr/bin/env bash
#
# build.sh - CodingFire for macOS
#
# Builds dist/CodingFire with the Go toolchain, the same shape as its sibling
# scripts/windows/build.ps1: vet first, then build with pinned flags, then
# report the version the source claims.
#
# Why this file exists at all. macOS is the one target that cannot be
# cross-compiled - the window layer is cgo (Cocoa/Objective-C), so it needs the
# local SDK and a real clang. That makes "build it" a thing you do on the
# machine you are sitting at, which is exactly what a script is for.
#
# CGO_ENABLED=1 IS NOT OPTIONAL, so this script sets it rather than trusting the
# caller's environment. With CGO_ENABLED=0 the build still SUCCEEDS and then the
# binary panics on launch with "no native backend available": without cgo, go-gui
# selects gui/backend/run_default.go instead of the metal backend. A build that
# compiles clean and dies at startup is the worst failure mode there is, and it
# is why the flag is pinned here and asserted below.
#
# Usage (run from the repo root, or anywhere - the repo root is resolved from
# this file's path, not from the caller's working directory):
#
#   scripts/macos/build.sh               build -> dist/CodingFire
#   scripts/macos/build.sh --run         build, then launch the tray app
#   scripts/macos/build.sh --dump FILE   build, then write the local-usage report
#   scripts/macos/build.sh --render DIR  build, then write the pixel-art previews
#   scripts/macos/build.sh --check       build, then prove it starts (headless)
#   scripts/macos/build.sh --universal   build one fat arm64 + x86_64 binary
#
# --universal is the local equivalent of a release build: the release matrix
# ships CodingFire-macos-x64 and CodingFire-macos-arm64 separately, and this
# joins them into the single binary you would hand someone. Everything else
# builds for the host architecture only.
#
# A plain build proves the code compiles. It does NOT prove the app starts -
# the NSWindow collectionBehavior crash that shipped in 1.1.3 compiled fine and
# died on the first frame. Use --check for that; it runs the headless report and
# asserts the output, which is the strongest check available without a display.
#
# This script is intentionally ASCII-only, like its PowerShell siblings.

set -euo pipefail

usage() {
    sed -n '3,42p' "${BASH_SOURCE[0]}" | sed -e 's/^#//' -e 's/^ //'
    exit "${1:-0}"
}

# ---------------------------------------------------------------------------
# Arguments
# ---------------------------------------------------------------------------
mode=build
target=''
universal=0

while [[ $# -gt 0 ]]; do
    case "$1" in
        --run)       mode=run ;;
        --check)     mode=check ;;
        --universal) universal=1 ;;
        --dump)
            mode=dump
            if [[ $# -gt 1 ]]; then target=$2; shift; else target="codingfire-dump.txt"; fi
            ;;
        --render)
            mode=render
            if [[ $# -gt 1 ]]; then target=$2; shift; else target="preview"; fi
            ;;
        -h|--help)   usage 0 ;;
        *)           echo "unknown argument: $1" >&2; usage 2 ;;
    esac
    shift
done

# ---------------------------------------------------------------------------
# Paths
#
# This file lives in scripts/macos/, so the repository root is two levels up.
# Resolving it from the script's own location rather than from the caller's
# working directory is what lets the script be run from anywhere; a build
# launched from $HOME would otherwise look for dist/ and internal/ next to $HOME.
# ---------------------------------------------------------------------------
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
dist="$root/dist"
app="$dist/CodingFire"
ldflags='-s -w'

# ---------------------------------------------------------------------------
# Toolchain
# ---------------------------------------------------------------------------
command -v go >/dev/null 2>&1 || {
    echo "No Go toolchain on PATH. Install Go 1.26 or later." >&2
    exit 1
}
echo "go       : $(go version)"

# cgo needs a real compiler behind it. Checking here turns a missing or broken
# Xcode install into one clear sentence instead of a wall of linker errors.
if ! xcrun --find clang >/dev/null 2>&1; then
    echo "clang not found. Install the command line tools: xcode-select --install" >&2
    exit 1
fi

if [[ $universal == 1 ]] && ! command -v lipo >/dev/null 2>&1; then
    echo "lipo not found (it ships with the command line tools), cannot build a universal binary." >&2
    exit 1
fi

# ---------------------------------------------------------------------------
# Vet first: `go build` catches type errors, `go vet` catches the ones that
# compile but are wrong (unreachable code, bad Printf verbs, lost lock copies).
#
# Every go invocation is wrapped in a subshell that cd's to $root. Without it
# `go vet ./...` would resolve against the caller's directory and fail with
# "directory prefix . does not contain main module" - the same trap the
# -WorkingDirectory flag exists to avoid in build.ps1.
# ---------------------------------------------------------------------------
echo "vetting  : ./..."
( cd "$root" && go vet ./... )

# ---------------------------------------------------------------------------
# Build
#
#   -trimpath        strips the local build path out of the binary, so two
#                    machines building the same tag produce the same bytes
#   -s -w            drop the symbol table and DWARF
#   CGO_ENABLED=1    the metal backend; see the note at the top of this file
#
# There is no macOS equivalent of -H=windowsgui: the binary is a plain Mach-O
# and macOS decides how to treat it from the bundle, which this app does not use.
# ---------------------------------------------------------------------------
build_arch() { # arch outfile
    ( cd "$root" && CGO_ENABLED=1 GOARCH="$1" go build -trimpath -ldflags "$ldflags" -o "$2" . )
}

mkdir -p "$dist"

# Delete the previous output before building. Not cosmetic:
# `go build -o` inspects an existing output file and refuses to overwrite a
# universal (fat) Mach-O, failing with "already exists and is not an object
# file" - a message that says nothing about the real cause. Since --universal
# produces exactly that, a plain build run after a --universal one would die on
# it. build.ps1 deletes its exe for the same class of reason.
#
# On macOS the delete always succeeds, even while the app is running: the
# running process keeps its own copy of the old inode. So a live instance will
# quietly go on running the previous build, which is worth saying out loud.
if [[ -f $app ]]; then
    if pgrep -x CodingFire >/dev/null 2>&1; then
        echo "warning  : CodingFire is running - it keeps the old binary until you quit it" >&2
    fi
    rm -f "$app"
fi

host_arch=$(go env GOHOSTARCH)

if [[ $universal == 1 ]]; then
    echo "building : CodingFire (universal: arm64 + x86_64)"
    staging=$(mktemp -d)
    # A failure mid-way must not leave the staging directory behind.
    trap 'rm -rf "$staging"' EXIT

    build_arch arm64 "$staging/CodingFire-arm64"
    build_arch amd64 "$staging/CodingFire-x64"
    lipo -create -output "$app" "$staging/CodingFire-arm64" "$staging/CodingFire-x64"

    rm -rf "$staging"
    trap - EXIT
else
    echo "building : CodingFire ($host_arch)"
    build_arch "$host_arch" "$app"
fi

[[ -f "$app" ]] || { echo "BUILD FAILED (no output file)" >&2; exit 1; }

size_kb=$(( $(stat -f%z "$app") / 1024 ))
echo "built    : $app (${size_kb} KB)"

if [[ $universal == 1 ]]; then
    echo "archs    : $(lipo -archs "$app")"
else
    echo "arch     : $(file -b "$app" | sed 's/^.*: //')"
fi

# ---------------------------------------------------------------------------
# Report the version the source claims.
#
# The version lives in exactly one place (internal/core/version.go), and a Go
# binary has no version resource to read it back from - so it is read from the
# source. The release workflow's verify job asserts the same constant against the
# tag being released, which is what stops "forgot to bump the version" from
# reaching a user who then reports a bug against the wrong build.
# ---------------------------------------------------------------------------
verfile="$root/internal/core/version.go"
ver=''
if [[ -f $verfile ]]; then
    ver=$(sed -n 's/.*Version[[:space:]]*=[[:space:]]*"\([^"]*\)".*/\1/p' "$verfile" | head -1)
fi
if [[ -n $ver ]]; then
    echo "version  : $ver"
else
    echo "version  : (could not read internal/core/version.go)"
fi

# ---------------------------------------------------------------------------
# Modes
# ---------------------------------------------------------------------------

# --check runs the headless report and asserts its shape. This is the same
# assertion the macOS CI job makes, and it is positive on purpose: a dump that
# was never written would also satisfy "the file has no error in it".
if [[ $mode == check ]]; then
    echo "checking : headless report"
    workdir=$(mktemp -d)
    trap 'rm -rf "$workdir"' EXIT

    # CODINGFIRE_DATA_DIR keeps the run out of the real user's data and gives
    # the report a known path to assert against.
    CODINGFIRE_DATA_DIR="$workdir/data" "$app" --dump "$workdir/dump.txt"
    head -3 "$workdir/dump.txt"

    grep -q '^CodingFire ' "$workdir/dump.txt" || {
        echo "FAIL: the report has no header line" >&2
        exit 1
    }
    # The report lists one line per collected source. A run that collected
    # nothing would still produce a well-formed report, so the count is what
    # makes this a real check. The list is static, so 10 is a floor rather than
    # a number that moves with the machine.
    sources=$(grep -c '^  [A-Za-z]' "$workdir/dump.txt" || true)
    echo "sources  : $sources"
    if [[ $sources -lt 10 ]]; then
        echo "FAIL: expected the full source list, got $sources" >&2
        cat "$workdir/dump.txt" >&2
        exit 1
    fi
    echo "ok: the binary starts and reaches the data layer"
    exit 0
fi

# Relative targets are resolved against the caller's directory, like the binary
# argument to scripts/linux/linux-smoke.sh, so a bare `--dump out.txt` lands
# somewhere predictable rather than next to the script.
case "$target" in
    /*) ;;
    '') ;;
    *)  target="$PWD/$target" ;;
esac

case "$mode" in
    dump)
        "$app" --dump "$target"
        ;;
    render)
        "$app" --render "$target"
        ;;
    run)
        # Detached on purpose: this is a tray app, and the whole point of
        # --run is to get it on screen and hand the terminal back.
        nohup "$app" >/dev/null 2>&1 &
        echo "launched : pid $!"
        ;;
esac
