# build.ps1 - CodingFire for Windows (Go)
#
# Builds dist\CodingFire.exe with the Go toolchain.
#
# Unlike the C# build there is nothing to search for and no reference-assembly
# dance: Go is a single self-contained toolchain, the binary is static, and the
# only inputs are the .go files. What this script is actually for is pinning the
# build flags so every build is identical, and reporting the version the binary
# claims so a build log says which build it produced.
#
#   default   -> dist\CodingFire.exe
#   -Run      build, then launch the tray app
#   -Dump     build, then write the local-usage report
#   -Render   build, then write the pixel-art preview PNGs
#
# Usage (run from the repo root, or anywhere - paths are resolved from
# $PSScriptRoot, not from the caller's working directory):
#   powershell -ExecutionPolicy Bypass -File scripts\windows\build.ps1
#   powershell -ExecutionPolicy Bypass -File scripts\windows\build.ps1 -Run
#
# Target: Windows 10 and later, x64. Go dropped Windows 7/8 support in 1.21, so
# this build cannot serve the Win7 SP1 range - that is what the C# build in the
# sibling repository is for. The two are meant to coexist.
#
# This script is intentionally ASCII-only so PowerShell 5.1 never mis-decodes it.

[CmdletBinding()]
param(
    [switch]$Run,
    [switch]$Dump,
    [switch]$Render
)

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

# Some hosts export the proxy settings twice under different casing (`http_proxy`
# AND `HTTP_PROXY`). PowerShell 5.1's process launcher builds a case-SENSITIVE
# dictionary out of the environment block and then dies with a duplicate-key
# error. Drop the duplicates through the .NET API (the `$env:` provider is
# case-insensitive and does not reliably clear both).
foreach ($v in @('http_proxy', 'https_proxy', 'HTTP_PROXY', 'HTTPS_PROXY')) {
    [System.Environment]::SetEnvironmentVariable($v, $null)
}

# This file lives in scripts\windows\, so the repository root is two levels up.
# Resolving it from $PSScriptRoot rather than from the caller's working directory
# is what lets the script be run from anywhere: launched from C:\ it would
# otherwise look for dist\ and internal\ next to C:\ instead of next to the repo.
$root    = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
$distDir = Join-Path $root 'dist'
$exePath = Join-Path $distDir 'CodingFire.exe'
$tmp     = [System.IO.Path]::GetTempPath()

# Some Go installations are placed below Program Files and inherit a module or
# build cache there. That location is not writable for a normal user, which
# makes vet fail before the compiler reaches the dist output. Keep both caches
# in the user's temp directory unless the caller intentionally supplied them.
$cacheRoot = Join-Path $tmp 'CodingFireGo-cache'
if (-not $env:GOMODCACHE) { $env:GOMODCACHE = Join-Path $cacheRoot 'mod' }
if (-not $env:GOCACHE) { $env:GOCACHE = Join-Path $cacheRoot 'build' }
[System.IO.Directory]::CreateDirectory($env:GOMODCACHE) | Out-Null
[System.IO.Directory]::CreateDirectory($env:GOCACHE) | Out-Null

function Find-Go {
    # Every candidate is checked for emptiness first: some hosts (and sandboxes)
    # leave %ProgramFiles(x86)% unset, and Join-Path / Test-Path throw a
    # parameter-binding error on a null path rather than reporting "not found".
    $candidates = @()
    foreach ($base in @($env:ProgramFiles, ${env:ProgramFiles(x86)}, 'D:\Program Files (x86)', 'C:\Program Files')) {
        if ($base) { $candidates += (Join-Path $base 'Go\bin\go.exe') }
    }
    foreach ($c in $candidates) {
        if (Test-Path -LiteralPath $c) { return $c }
    }
    $cmd = Get-Command go -ErrorAction SilentlyContinue
    if ($cmd) { return $cmd.Source }
    throw "No Go toolchain found. Install Go 1.26 or later, or put go.exe on PATH."
}

# Invoke-Go runs the toolchain through Start-Process and returns the exit code.
#
# It does NOT use the call operator. `& $go ...` inside a .ps1 gets swallowed by
# the host here: no output, and $LASTEXITCODE stays EMPTY rather than becoming
# non-zero, so a failed build reads as a successful one. Start-Process hands back
# the real exit code plus both streams, which is the only way to tell the
# difference. Same reason the C# build.ps1 launches csc this way.
function Invoke-Go {
    param([string[]]$GoArgs)

    $soFile = Join-Path $tmp 'codingfire-out.txt'
    $seFile = Join-Path $tmp 'codingfire-err.txt'
    [System.IO.File]::Delete($soFile)
    [System.IO.File]::Delete($seFile)

    # Start-Process joins -ArgumentList with spaces and does not quote, so any
    # argument containing a space has to be quoted by hand.
    $quoted = @()
    foreach ($a in $GoArgs) {
        if ($a -match '\s') { $quoted += ('"' + $a + '"') } else { $quoted += $a }
    }

    # -WorkingDirectory matters: Start-Process does not inherit the caller's
    # location, so `go vet ./...` would resolve against whatever directory the
    # host happened to start in and fail with "directory prefix . does not
    # contain main module".
    $proc = Start-Process -FilePath $script:go -ArgumentList $quoted -NoNewWindow -Wait -PassThru `
                          -WorkingDirectory $script:root `
                          -RedirectStandardOutput $soFile -RedirectStandardError $seFile

    $lines = @()
    if (Test-Path -LiteralPath $soFile) { $lines += [System.IO.File]::ReadAllLines($soFile) }
    if (Test-Path -LiteralPath $seFile) { $lines += [System.IO.File]::ReadAllLines($seFile) }
    return @{ ExitCode = $proc.ExitCode; Output = ($lines -join "`n") }
}

$script:go = Find-Go
$script:root = $root
Write-Host "go       : $script:go" -ForegroundColor DarkGray

$verResult = Invoke-Go @('version')
if ($verResult.ExitCode -ne 0) {
    Write-Host $verResult.Output
    throw "go version failed (exit $($verResult.ExitCode))."
}
Write-Host "toolchain: $($verResult.Output.Trim())" -ForegroundColor DarkGray

if (-not (Test-Path -LiteralPath $distDir)) {
    [System.IO.Directory]::CreateDirectory($distDir) | Out-Null
}

# A running CodingFire.exe holds its own image open, so the replace fails with a
# bare "access denied" and the script dies with no compiler output at all - which
# reads exactly like a mystery build failure. Say what is actually wrong.
if (Test-Path -LiteralPath $exePath) {
    try {
        [System.IO.File]::Delete($exePath)
    } catch {
        throw "Cannot replace $exePath - it is still running. Quit CodingFire (tray icon -> Quit) and build again."
    }
}

# ---------------------------------------------------------------------------
# Vet first: `go build` catches type errors, `go vet` catches the ones that
# compile but are wrong (unreachable code, bad Printf verbs, lost lock copies).
# ---------------------------------------------------------------------------
Write-Host "vetting  : ./..." -ForegroundColor DarkGray
$vet = Invoke-Go @('vet', './...')
if ($vet.ExitCode -ne 0) {
    Write-Host $vet.Output
    Write-Host "VET FAILED" -ForegroundColor Red
    exit 1
}

# ---------------------------------------------------------------------------
# Build.
#
#   -trimpath            strips the local build path out of the binary, so two
#                        machines building the same tag produce the same bytes
#   -s -w                drop the symbol table and DWARF: about 4 MB here
#   -H=windowsgui        mark the image as a GUI subsystem app, so launching it
#                        does not leave a console window behind. The headless
#                        modes still write their files; they just do not have
#                        anywhere to print when started from Explorer.
#   CGO_ENABLED=0        no cgo, which is what keeps the binary self-contained
# ---------------------------------------------------------------------------
$env:CGO_ENABLED = '0'
$ldflags = '-s -w -H=windowsgui'
Write-Host "building : CodingFire.exe" -ForegroundColor DarkGray

$build = Invoke-Go @('build', '-trimpath', '-ldflags', $ldflags, '-o', $exePath, '.')
if ($build.ExitCode -ne 0) {
    Write-Host $build.Output
    Write-Host "BUILD FAILED" -ForegroundColor Red
    exit 1
}
if (-not (Test-Path -LiteralPath $exePath)) {
    Write-Host "BUILD FAILED (no output file)" -ForegroundColor Red
    exit 1
}

$sizeKb = [math]::Round((Get-Item -LiteralPath $exePath).Length / 1KB, 1)
Write-Host "built    : $exePath ($sizeKb KB)" -ForegroundColor Green

# ---------------------------------------------------------------------------
# Report the version the source claims.
#
# The version lives in exactly one place (internal\core\version.go), and a Go
# binary has no version resource to read it back from - so it is read from the
# source. The release workflow's verify job asserts the same constant against the
# tag being released, which is what stops "forgot to bump the version" from
# reaching a user who then reports a bug against the wrong build.
# ---------------------------------------------------------------------------
$verFile = Join-Path $root 'internal\core\version.go'
$ver = ''
if (Test-Path -LiteralPath $verFile) {
    $m = [regex]::Match((Get-Content -LiteralPath $verFile -Raw), 'Version\s*=\s*"([^"]+)"')
    if ($m.Success) { $ver = $m.Groups[1].Value }
}
if ($ver.Length -gt 0) {
    Write-Host "version  : $ver" -ForegroundColor Green
} else {
    Write-Host "version  : (could not read internal\core\version.go)" -ForegroundColor DarkYellow
}

if ($Dump) {
    & $exePath --dump (Join-Path $root 'codingfire-dump.txt')
} elseif ($Render) {
    & $exePath --render (Join-Path $root 'preview')
} elseif ($Run) {
    Start-Process -FilePath $exePath | Out-Null
}
