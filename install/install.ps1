param(
    [string]$BinDir = "$HOME\AppData\Local\Programs\unch\bin",
    [string]$Version = ""
)

$ErrorActionPreference = "Stop"

$Repo = "uchebnick/unch"

function Write-Note {
    param([string]$Message)
    Write-Host $Message
}

function Normalize-Version {
    param([string]$RequestedVersion)
    if ([string]::IsNullOrWhiteSpace($RequestedVersion) -or $RequestedVersion -eq "latest") {
        return "latest"
    }
    if ($RequestedVersion.StartsWith("v")) {
        return $RequestedVersion
    }
    return "v$RequestedVersion"
}

function Resolve-LatestVersion {
    try {
        $response = Invoke-WebRequest -Method Head -MaximumRedirection 0 -Uri "https://github.com/$Repo/releases/latest" -ErrorAction Stop
        return "latest"
    } catch {
        $location = $_.Exception.Response.Headers.Location
        if ($location) {
            return [System.IO.Path]::GetFileName($location.ToString())
        }
        return "latest"
    }
}

function Resolve-AssetArchive {
    param(
        [string]$ResolvedVersion,
        [string]$AssetName,
        [string]$DestinationPath
    )

    $assetDir = $env:UNCH_INSTALL_ASSET_DIR
    if (-not [string]::IsNullOrWhiteSpace($assetDir)) {
        $localAsset = Join-Path $assetDir $AssetName
        if (Test-Path $localAsset) {
            Write-Note "Using local install asset $localAsset"
            return $localAsset
        }
    }

    $url = "https://github.com/$Repo/releases/download/$ResolvedVersion/$AssetName"
    Write-Note "Downloading $url"
    Invoke-WebRequest -Uri $url -OutFile $DestinationPath
    return $DestinationPath
}

function Detect-Arch {
    $arch = $env:PROCESSOR_ARCHITECTURE
    if (-not [string]::IsNullOrWhiteSpace($env:PROCESSOR_ARCHITEW6432)) {
        $arch = $env:PROCESSOR_ARCHITEW6432
    }

    switch ($arch.ToUpperInvariant()) {
        "AMD64" { return "x86_64" }
        "ARM64" { return "arm64" }
        default { return "unknown" }
    }
}

function Install-ReleaseArchive {
    param(
        [string]$ResolvedVersion,
        [string]$Destination
    )

    $arch = Detect-Arch
    $asset = "unch_Windows_${arch}.zip"
    $tmpDir = Join-Path ([System.IO.Path]::GetTempPath()) ([System.Guid]::NewGuid().ToString())
    New-Item -ItemType Directory -Force -Path $tmpDir | Out-Null
    try {
        $archive = Resolve-AssetArchive -ResolvedVersion $ResolvedVersion -AssetName $asset -DestinationPath (Join-Path $tmpDir $asset)
        Expand-Archive -Path $archive -DestinationPath $tmpDir -Force
        New-Item -ItemType Directory -Force -Path $Destination | Out-Null
        Copy-Item -Path (Join-Path $tmpDir "unch.exe") -Destination (Join-Path $Destination "unch.exe") -Force
        return $true
    } catch {
        return $false
    } finally {
        Remove-Item -Recurse -Force $tmpDir -ErrorAction SilentlyContinue
    }
}

function Install-WithGo {
    param(
        [string]$ResolvedVersion,
        [string]$Destination
    )

    $go = Get-Command go -ErrorAction SilentlyContinue
    if (-not $go) {
        return $false
    }

    if ($ResolvedVersion -eq "latest") {
        $pkgVersion = "@latest"
    } else {
        $pkgVersion = "@$ResolvedVersion"
    }

    Write-Note "Installing via go install github.com/$Repo$pkgVersion"
    New-Item -ItemType Directory -Force -Path $Destination | Out-Null
    $env:GOBIN = $Destination
    & go install "github.com/$Repo$pkgVersion"
    return $LASTEXITCODE -eq 0
}

$resolvedVersion = Normalize-Version $Version
if ($resolvedVersion -eq "latest") {
    $resolvedVersion = Resolve-LatestVersion
}

$installed = $false

if ($resolvedVersion -ne "latest" -or -not [string]::IsNullOrWhiteSpace($env:UNCH_INSTALL_ASSET_DIR)) {
    $installed = Install-ReleaseArchive -ResolvedVersion $resolvedVersion -Destination $BinDir
}

if (-not $installed) {
    $installed = Install-WithGo -ResolvedVersion $resolvedVersion -Destination $BinDir
}

if (-not $installed) {
    throw "Could not install unch. Windows release archives are currently published for x86_64 only; otherwise install Go and rerun this script."
}

Write-Note "Installed unch to $(Join-Path $BinDir 'unch.exe')"
if (-not ($env:Path -split ';' | Where-Object { $_ -eq $BinDir })) {
    Write-Note "Note: $BinDir is not currently on PATH."
}
