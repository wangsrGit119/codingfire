# release.ps1 - CodingFire for Windows (Go)
#
# Builds, packages and publishes a GitHub release in one go:
#   version check -> build -> zip -> annotated tag -> push tag -> release
#
# The zip is the release asset. dist\ and *.exe/*.zip are gitignored, so the
# binary never enters git history - only the tag and the release point at it.
#
# Usage (run from the repo root, or anywhere - paths are resolved from $PSScriptRoot):
#   powershell -ExecutionPolicy Bypass -File release.ps1 -Version 1.0.4
#   powershell -ExecutionPolicy Bypass -File release.ps1 -Version 1.0.4 -SkipBuild
#   powershell -ExecutionPolicy Bypass -File release.ps1 -Version 1.0.4 -NoPush
#
# -SkipBuild   reuse an existing dist\CodingFire.exe instead of rebuilding
# -NoPush      do everything locally (zip + tag) and print the push commands
# -Notes       release notes body; a sensible default is generated if omitted
# -Force       overwrite an existing local tag of the same name
#
# Requires: git. The gh CLI is optional - without it the script prints the exact
# web-UI steps instead of creating the release for you.
#
# NOTE ON GIT IN SANDBOXES: git is blocked by name inside the WorkBuddy shell, so
# this script cannot be run end to end there. Run it from a normal terminal. The
# tag/push steps are plain `git` calls for that reason - there is no way to do
# them from inside the sandbox anyway.
#
# This script is intentionally ASCII-only so PowerShell 5.1 never mis-decodes it.

[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$Version,
    [string]$Notes = '',
    [switch]$SkipBuild,
    [switch]$NoPush,
    [switch]$Force
)

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

foreach ($v in @('http_proxy', 'https_proxy', 'HTTP_PROXY', 'HTTPS_PROXY')) {
    [System.Environment]::SetEnvironmentVariable($v, $null)
}

function Step($m) { Write-Host "==> $m" -ForegroundColor Cyan }
function Ok($m) { Write-Host "    $m" -ForegroundColor Green }
function Warn($m) { Write-Host "    $m" -ForegroundColor Yellow }

$root = $PSScriptRoot
$distDir = Join-Path $root 'dist'
$exePath = Join-Path $distDir 'CodingFire.exe'

# ---------------------------------------------------------------------------
# 1. Version + tag name
# ---------------------------------------------------------------------------
$ver = $Version.Trim()
if ($ver.StartsWith('v')) { $ver = $ver.Substring(1) }
if ($ver -notmatch '^\d+\.\d+\.\d+$') {
    throw "Version must look like 1.0.4 (got '$Version')."
}
$tag = "v$ver"
$zipName = "CodingFire-win-x64-$tag.zip"
$zipPath = Join-Path $root $zipName

Write-Host "version  : $ver" -ForegroundColor DarkGray
Write-Host "tag      : $tag" -ForegroundColor DarkGray
Write-Host "asset    : $zipName" -ForegroundColor DarkGray

# ---------------------------------------------------------------------------
# 2. The source must claim the version we are about to tag.
#
# The version lives in exactly one place (internal\core\version.go). Forgetting
# to bump it is the kind of mistake you only notice when someone reports a bug
# against "1.0.3" that was actually fixed in 1.0.4 - so refuse to tag instead.
#
# The C# build reads this back off the exe's version resource; a Go binary has no
# such resource, so the constant is read from source. Same guarantee, and it is
# checked before the build rather than after, so a mismatch costs no build time.
# ---------------------------------------------------------------------------
Step "Checking internal\core\version.go"
$verFile = Join-Path $root 'internal\core\version.go'
if (-not (Test-Path -LiteralPath $verFile)) {
    throw "Not found: $verFile. Is this the CodingFire repository root?"
}
$m = [regex]::Match((Get-Content -LiteralPath $verFile -Raw), 'Version\s*=\s*"([^"]+)"')
if (-not $m.Success) {
    throw "Could not parse a Version constant out of $verFile."
}
$declared = $m.Groups[1].Value
if ($declared -ne $ver) {
    throw @"
Version mismatch: the source declares '$declared' but you are releasing '$ver'.

Bump internal\core\version.go first:
    const Version = "$ver"
"@
}
Ok "source declares $declared"

# ---------------------------------------------------------------------------
# 3. Sanity: must be a git repo, working tree clean, tag must not exist yet
# ---------------------------------------------------------------------------
Step "Checking the working tree"
if (-not (Test-Path -LiteralPath (Join-Path $root '.git'))) {
    throw "$root is not a git repository."
}

$dirty = (& git -C $root status --porcelain) -join "`n"
if ($dirty.Trim().Length -ne 0) {
    Warn "uncommitted changes:"
    Write-Host $dirty
    throw "Commit or stash before releasing - the tag would not describe what was built."
}
Ok "clean"

$existing = (& git -C $root tag -l $tag) -join ''
if ($existing.Trim().Length -ne 0) {
    if (-not $Force) {
        throw "Tag $tag already exists. Pass -Force to replace it, or pick a new version."
    }
    # A release that already points at this tag would be deleted along with it,
    # so say so before doing anything destructive.
    Warn "deleting existing local tag $tag (-Force)"
    & git -C $root tag -d $tag | Out-Null
    if ($LASTEXITCODE -ne 0) { throw "Could not delete tag $tag." }
}
Ok "tag $tag is free"

# ---------------------------------------------------------------------------
# 4. Build
# ---------------------------------------------------------------------------
if ($SkipBuild) {
    Step "-SkipBuild given - reusing $exePath"
    if (-not (Test-Path -LiteralPath $exePath)) {
        throw "$exePath does not exist. Run build.ps1 first, or drop -SkipBuild."
    }
} else {
    Step "Building"
    & (Join-Path $root 'build.ps1')
    if ($LASTEXITCODE -ne 0) { throw "build.ps1 failed (exit $LASTEXITCODE)." }
}

$sizeKb = [math]::Round((Get-Item -LiteralPath $exePath).Length / 1KB, 1)
Ok "$exePath ($sizeKb KB)"

# ---------------------------------------------------------------------------
# 5. Package the release asset
#
# The Go build has no .config sidecar - one static exe is the whole product - so
# the zip holds exactly one file. That is deliberate: it makes "unpack and run"
# literally true, with nothing to keep alongside the binary.
# ---------------------------------------------------------------------------
Step "Packaging $zipName"
Compress-Archive -LiteralPath @($exePath) -DestinationPath $zipPath -Force
$zipKb = [math]::Round((Get-Item -LiteralPath $zipPath).Length / 1KB, 1)
Ok "$zipPath ($zipKb KB)"

# ---------------------------------------------------------------------------
# 6. Annotated tag
# ---------------------------------------------------------------------------
Step "Tagging $tag"
& git -C $root tag -a $tag -m "CodingFire for Windows (Go) $tag"
if ($LASTEXITCODE -ne 0) { throw "git tag failed (exit $LASTEXITCODE)." }
Ok "tagged $(git -C $root rev-parse --short HEAD)"

# ---------------------------------------------------------------------------
# 7. Push the tag
# ---------------------------------------------------------------------------
$remote = (& git -C $root remote get-url origin) -join ''
$remote = $remote.Trim()
Write-Host "    remote: $remote" -ForegroundColor DarkGray

if ($NoPush) {
    Step "-NoPush given - stopping before push"
    Warn "push the tag yourself when ready:"
    Warn "  git push origin $tag"
} else {
    Step "Pushing tag"
    & git -C $root push origin $tag
    if ($LASTEXITCODE -ne 0) {
        Warn "git push failed (exit $LASTEXITCODE). The tag exists locally; retry with:"
        Warn "  git push origin $tag"
    } else {
        Ok "pushed $tag"
    }
}

# ---------------------------------------------------------------------------
# 8. Create the release
# ---------------------------------------------------------------------------
if ($Notes.Trim().Length -eq 0) {
    $Notes = @"
CodingFire for Windows (Go) $tag

Download the zip, unpack it anywhere and run CodingFire.exe - no installer, no
runtime, no DLLs.

* Single static executable, ~18 MB
* Windows 10 and later, x64
* 23 read-only local data sources, including WorkBuddy (CN) and WorkBuddy (INTL)
  counted separately
* Same --dump report format as the C# build, byte for byte

The C# build remains the choice for Windows 7 SP1; this one trades that range for
a self-contained binary and no .NET dependency.
"@
}

$gh = Get-Command gh -ErrorAction SilentlyContinue
if ($gh) {
    Step "Creating release via gh"
    & gh release create $tag $zipPath --title "CodingFire for Windows (Go) $tag" --notes $Notes
    if ($LASTEXITCODE -ne 0) { throw "gh release create failed (exit $LASTEXITCODE)." }
    Ok "release $tag published"
} else {
    Step "gh CLI not found - finish in the browser"
    Warn "1. open: $($remote -replace '\.git$', '')/releases/new?tag=$tag"
    Warn "2. set the title to: CodingFire for Windows (Go) $tag"
    Warn "3. attach this file: $zipPath"
    Warn "4. paste the notes below, then click 'Publish release'"
    Warn ""
    Warn "   (or use the REST API: POST /repos/{owner}/{repo}/releases with"
    Warn "    tag_name=$tag, and upload $zipName as an asset)"
    Write-Host ''
    Write-Host $Notes
    Write-Host ''
}

Write-Host "Done." -ForegroundColor Green
