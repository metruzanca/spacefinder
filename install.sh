#!/bin/sh
# spacefinder installer.
#
# This single file runs under both POSIX sh (`curl ... | sh`) and PowerShell
# (`irm ... | iex`), thanks to the polyglot header below. Keep the header and
# the two bodies in sync when editing.

echo \" <<'RUN_AS_BATCH' >/dev/null ">NUL "\" \`" <#"
<#
RUN_AS_BATCH
#> | Out-Null

echo \" <<'RUN_AS_POWERSHELL' >/dev/null # " | Out-Null
$ErrorActionPreference = 'Stop'
$Repo    = 'metruzanca/spacefinder'
$BinName = 'spacefinder'
$Base    = "https://github.com/$Repo/releases"

function Die([string]$msg) { Write-Error $msg; exit 1 }

function Get-LatestVersion {
    $uri = "$Base/latest"
    try {
        $null = Invoke-WebRequest -Uri $uri -MaximumRedirection 0 -UseBasicParsing -ErrorAction Stop
    } catch {
        $loc = $_.Exception.Response.Headers.Location
        if ($loc) { return [System.IO.Path]::GetFileName($loc) }
    }
    $release = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest" -Headers @{ 'User-Agent' = 'spacefinder-installer' }
    return $release.tag_name
}

Write-Output 'Determining latest release...'
$version = Get-LatestVersion
if (-not $version) { Die 'could not determine the latest version (network issue?)' }
$ver = $version.TrimStart('v')

if ($env:OS -eq 'Windows_NT') { $os = 'windows' }
elseif ($IsLinux)             { $os = 'linux' }
elseif ($IsMacOS)             { $os = 'darwin' }
else                          { Die 'unsupported OS' }

if ($os -eq 'windows') {
    $arch = $env:PROCESSOR_ARCHITECTURE
    if ($arch -match '^(AMD64|IA64|X86)$') { $archName = 'amd64' }
    elseif ($arch -match '^(ARM64|ARM)$')   { $archName = 'arm64' }
    else { Die "unsupported architecture: $arch" }
} else {
    $uarch = (uname -m).Trim().ToLower()
    if ($uarch -in @('x86_64', 'amd64'))      { $archName = 'amd64' }
    elseif ($uarch -in @('aarch64', 'arm64')) { $archName = 'arm64' }
    else { Die "unsupported architecture: $uarch" }
}

$ext     = if ($os -eq 'windows') { 'zip' } else { 'tar.gz' }
$archive = "${BinName}_${ver}_${os}_${archName}.$ext"
$url     = "$Base/download/$version/$archive"
$tmp     = Join-Path ([System.IO.Path]::GetTempPath()) "spacefinder-$([guid]::NewGuid().ToString('N'))"
New-Item -ItemType Directory -Path $tmp | Out-Null

Write-Output "Downloading $archive..."
$archivePath = Join-Path $tmp $archive
Invoke-WebRequest -Uri $url -OutFile $archivePath -UseBasicParsing
$checksumsPath = Join-Path $tmp 'checksums.txt'
Invoke-WebRequest -Uri "$Base/download/$version/${BinName}_${ver}_checksums.txt" -OutFile $checksumsPath -UseBasicParsing

Write-Output 'Verifying checksum...'
$line = Get-Content $checksumsPath | Where-Object { $_ -match "\s$([regex]::Escape($archive))$" } | Select-Object -First 1
if (-not $line) { Die "checksum not found for $archive" }
$expected = ($line -split '\s+')[0].ToLower()
$actual   = (Get-FileHash -Path $archivePath -Algorithm SHA256).Hash.ToLower()
if ($actual -ne $expected) { Die 'checksum verification failed' }

if ($os -eq 'windows') {
    Expand-Archive -Path $archivePath -DestinationPath $tmp -Force
    $bin = Join-Path $tmp "$BinName.exe"
} else {
    tar -xzf $archivePath -C $tmp
    $bin = Join-Path $tmp $BinName
}

if ($env:BINDIR) { $bindir = $env:BINDIR }
elseif ($env:PREFIX) { $bindir = Join-Path $env:PREFIX 'bin' }
elseif ($os -eq 'windows') { $bindir = Join-Path $env:LOCALAPPDATA "$BinName\bin" }
else { $bindir = Join-Path $HOME '.local/bin' }

New-Item -ItemType Directory -Path $bindir -Force | Out-Null
Copy-Item -Path $bin -Destination (Join-Path $bindir (Split-Path -Leaf $bin)) -Force
Write-Output "Installed ${BinName} ${version} to $bindir"

if ($os -eq 'windows') {
    $userPath = [Environment]::GetEnvironmentVariable('PATH', 'User')
    if ($userPath -notlike "*$bindir*") {
        Write-Output "Note: $bindir is not on your PATH. Add it, e.g.:"
        $hint = "  setx PATH `"$bindir;%PATH%`""
        Write-Output $hint
    }
} elseif ($env:PATH -notlike "*$bindir*") {
    Write-Output "Note: $bindir is not on your PATH; add it, e.g.:"
    $hint = "  echo 'export PATH=`"${bindir}:`$PATH`"' >> ~/.profile"
    Write-Output $hint
}

exit
<#
RUN_AS_POWERSHELL

set -eu

REPO="metruzanca/spacefinder"
BIN_NAME="spacefinder"
BASE="https://github.com/${REPO}/releases"

: "${BINDIR:="${PREFIX:-$HOME/.local}/bin"}"

log() { printf '%s\n' "$*"; }
die() { log "error: $*" >&2; exit 1; }

tmpdir="$(mktemp -d "${TMPDIR:-/tmp}/spacefinder.XXXXXX")"
trap 'rm -rf "$tmpdir"' EXIT INT TERM

log "Determining latest release..."
version="$(
  curl -fsSIL "${BASE}/latest" \
    | tr -d '\r' \
    | sed -n 's#^[Ll]ocation: .*/tag/\([^ ]*\).*#\1#p' \
    | head -n 1
)"
[ -n "$version" ] || die "could not determine the latest version (network issue?)"
ver="${version#v}"

os_name="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$os_name" in
  linux|darwin) ;;
  *) die "unsupported OS: $os_name" ;;
esac

arch="$(uname -m)"
case "$arch" in
  x86_64|amd64)   arch_name="amd64" ;;
  aarch64|arm64)  arch_name="arm64" ;;
  *) die "unsupported architecture: $arch" ;;
esac

archive="${BIN_NAME}_${ver}_${os_name}_${arch_name}.tar.gz"
url="${BASE}/download/${version}/${archive}"

log "Downloading ${archive}..."
cd "$tmpdir"
curl -fsSL "$url" -o "$archive"
curl -fsSL "${BASE}/download/${version}/${BIN_NAME}_${ver}_checksums.txt" -o checksums.txt

log "Verifying checksum..."
if command -v sha256sum >/dev/null 2>&1; then
  sha256sum -c --ignore-missing checksums.txt >/dev/null
else
  shasum -a 256 -c --ignore-missing checksums.txt >/dev/null
fi

tar -xzf "$archive" "$BIN_NAME"

mkdir -p "$BINDIR"
install -m 0755 "$BIN_NAME" "$BINDIR/$BIN_NAME"

log "Installed ${BIN_NAME} ${version} to ${BINDIR}/${BIN_NAME}"

if ! printf '%s' "$PATH" | tr ':' '\n' | grep -qFx "$BINDIR"; then
  log "Note: ${BINDIR} is not on your PATH; add it, e.g.:"
  log "  echo 'export PATH=\"${BINDIR}:\$PATH\"' >> ~/.profile"
fi

exit $?
#>