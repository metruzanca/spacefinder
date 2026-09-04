# spacefinder installer for Windows (PowerShell).
#
#   irm https://raw.githubusercontent.com/metruzanca/spacefinder/main/.github/install.ps1 | iex
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