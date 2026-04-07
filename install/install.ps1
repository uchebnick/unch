param(
    [string]$BinDir = "$HOME\AppData\Local\Programs\unch\bin",
    [string]$Version = ""
)

$ErrorActionPreference = "Stop"

$Repo = "uchebnick/unch"
$SourcePackage = "github.com/uchebnick/unch/cmd/unch"

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

function Get-ReleaseDownloadUrl {
    param(
        [string]$ResolvedVersion,
        [string]$FileName
    )

    if ($ResolvedVersion -eq "latest") {
        return "https://github.com/$Repo/releases/latest/download/$FileName"
    }

    return "https://github.com/$Repo/releases/download/$ResolvedVersion/$FileName"
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

    $url = Get-ReleaseDownloadUrl -ResolvedVersion $ResolvedVersion -FileName $AssetName
    Write-Note "Downloading $url"
    try {
        Invoke-WebRequest -Uri $url -OutFile $DestinationPath
    } catch {
        return $null
    }
    return $DestinationPath
}

function Resolve-ChecksumsFile {
    param(
        [string]$ResolvedVersion,
        [string]$DestinationPath
    )

    $assetDir = $env:UNCH_INSTALL_ASSET_DIR
    if (-not [string]::IsNullOrWhiteSpace($assetDir)) {
        $localChecksums = Join-Path $assetDir "checksums.txt"
        if (Test-Path $localChecksums) {
            return $localChecksums
        }
        throw "Missing checksums.txt in $assetDir"
    }

    $url = Get-ReleaseDownloadUrl -ResolvedVersion $ResolvedVersion -FileName "checksums.txt"
    Write-Note "Downloading $url"
    Invoke-WebRequest -Uri $url -OutFile $DestinationPath
    return $DestinationPath
}

function Find-ExpectedChecksum {
    param(
        [string]$ChecksumsPath,
        [string]$AssetName
    )

    foreach ($line in Get-Content -Path $ChecksumsPath) {
        if ($line -match '^\s*([0-9a-fA-F]+)\s+\*?(.+?)\s*$') {
            $fileName = [System.IO.Path]::GetFileName($Matches[2])
            if ($fileName -eq $AssetName) {
                return $Matches[1].ToLowerInvariant()
            }
        }
    }

    return $null
}

function Verify-AssetChecksum {
    param(
        [string]$AssetPath,
        [string]$AssetName,
        [string]$ChecksumsPath
    )

    $expected = Find-ExpectedChecksum -ChecksumsPath $ChecksumsPath -AssetName $AssetName
    if ([string]::IsNullOrWhiteSpace($expected)) {
        throw "Could not find a SHA-256 checksum for $AssetName in $ChecksumsPath"
    }

    $actual = (Get-FileHash -Path $AssetPath -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($actual -ne $expected) {
        throw "SHA-256 mismatch for $AssetName. Expected: $expected Actual: $actual"
    }
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
    if ($arch -eq "unknown") {
        return [pscustomobject]@{ Success = $false; Fatal = $false }
    }

    $asset = "unch_Windows_${arch}.zip"
    $tmpDir = Join-Path ([System.IO.Path]::GetTempPath()) ([System.Guid]::NewGuid().ToString())
    New-Item -ItemType Directory -Force -Path $tmpDir | Out-Null
    try {
        $archive = Resolve-AssetArchive -ResolvedVersion $ResolvedVersion -AssetName $asset -DestinationPath (Join-Path $tmpDir $asset)
        if (-not $archive) {
            return [pscustomobject]@{ Success = $false; Fatal = $false }
        }
        $checksums = Resolve-ChecksumsFile -ResolvedVersion $ResolvedVersion -DestinationPath (Join-Path $tmpDir "checksums.txt")
        Verify-AssetChecksum -AssetPath $archive -AssetName $asset -ChecksumsPath $checksums
        Expand-Archive -Path $archive -DestinationPath $tmpDir -Force
        New-Item -ItemType Directory -Force -Path $Destination | Out-Null
        Copy-Item -Path (Join-Path $tmpDir "unch.exe") -Destination (Join-Path $Destination "unch.exe") -Force
        return [pscustomobject]@{ Success = $true; Fatal = $false }
    } catch {
        Write-Error $_
        return [pscustomobject]@{ Success = $false; Fatal = $true }
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

    Write-Note "Installing via go install $SourcePackage$pkgVersion"
    New-Item -ItemType Directory -Force -Path $Destination | Out-Null
    $env:GOBIN = $Destination
    & go install "$SourcePackage$pkgVersion"
    return $LASTEXITCODE -eq 0
}

$resolvedVersion = Normalize-Version $Version

$installed = $false

$archiveInstall = Install-ReleaseArchive -ResolvedVersion $resolvedVersion -Destination $BinDir
$installed = $archiveInstall.Success
if (-not $installed -and $archiveInstall.Fatal) {
    throw "Release archive install failed verification or extraction; refusing to continue."
}

if (-not $installed) {
    $installed = Install-WithGo -ResolvedVersion $resolvedVersion -Destination $BinDir
}

if (-not $installed) {
    throw "Could not install unch. Install Go and rerun this script, or request a published release archive for your Windows architecture."
}

Write-Note "Installed unch to $(Join-Path $BinDir 'unch.exe')"
if (-not ($env:Path -split ';' | Where-Object { $_ -eq $BinDir })) {
    $env:Path = "$BinDir;$env:Path"
    Write-Note "Added $BinDir to PATH for the current PowerShell session."
    Write-Note "If you want to keep it available in future sessions, add $BinDir to your user PATH."
}
