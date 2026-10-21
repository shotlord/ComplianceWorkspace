<#
.SYNOPSIS
    Menyiapkan paket rilis Windows x64 dari executable yang dipilih secara eksplisit.
.PARAMETER ExecutablePath
    Jalur executable hasil build yang akan diverifikasi dan dikemas.
#>
param(
    [string]$ExecutablePath
)

$ErrorActionPreference = "Stop"

$targetVersion = "1.1.1"
$expectedPEVersion = "1.1.1.0"
$expectedProductName = "Compliance Workspace"

$workspaceRoot = $PSScriptRoot | Split-Path -Parent
$releaseDir = Join-Path $workspaceRoot "release"
$pkgDir = Join-Path $releaseDir "ComplianceWorkspace-v1.1.1-Windows-x64"
$zipPath = Join-Path $releaseDir "ComplianceWorkspace-v1.1.1-Windows-x64.zip"
$checksumPath = Join-Path $releaseDir "checksums-v1.1.1-sha256.txt"
$dstExe = Join-Path $pkgDir "ComplianceWorkspace.exe"
$srcReadme = Join-Path $PSScriptRoot "README.txt"
$srcVersion = Join-Path $workspaceRoot "VERSION.txt"

if ([string]::IsNullOrWhiteSpace($ExecutablePath)) {
    throw "Parameter -ExecutablePath wajib diisi dengan jalur executable hasil build v$targetVersion yang dipilih secara eksplisit."
}
if (-not (Test-Path -LiteralPath $ExecutablePath -PathType Leaf)) {
    throw "Executable sumber tidak ditemukan atau bukan file: '$ExecutablePath'."
}

$srcExe = (Resolve-Path -LiteralPath $ExecutablePath).Path
$srcExeItem = Get-Item -LiteralPath $srcExe -Force
if (($srcExeItem.Attributes -band [System.IO.FileAttributes]::ReparsePoint) -ne 0) {
    throw "Executable sumber tidak boleh berupa symbolic link atau reparse point: '$srcExe'."
}

$readStream = $null
try {
    $readStream = [System.IO.File]::Open(
        $srcExe,
        [System.IO.FileMode]::Open,
        [System.IO.FileAccess]::Read,
        [System.IO.FileShare]::Read
    )
    if (-not $readStream.CanRead) {
        throw "File tidak dapat dibaca."
    }
} catch {
    throw "Executable sumber tidak dapat dibaca: '$srcExe'. $($_.Exception.Message)"
} finally {
    if ($null -ne $readStream) {
        $readStream.Dispose()
    }
}

$versionInfo = [System.Diagnostics.FileVersionInfo]::GetVersionInfo($srcExe)
if ($versionInfo.ProductName -ne $expectedProductName) {
    throw "Executable sumber ditolak: ProductName '$($versionInfo.ProductName)' tidak sesuai dengan '$expectedProductName'."
}
if ($versionInfo.FileVersion -ne $expectedPEVersion) {
    throw "Executable sumber ditolak: FileVersion '$($versionInfo.FileVersion)' tidak sesuai dengan versi target '$expectedPEVersion'."
}
if ($versionInfo.ProductVersion -ne $expectedPEVersion) {
    throw "Executable sumber ditolak: ProductVersion '$($versionInfo.ProductVersion)' tidak sesuai dengan versi target '$expectedPEVersion'."
}

if (-not (Test-Path -LiteralPath $srcReadme -PathType Leaf)) {
    throw "Panduan paket tidak ditemukan: '$srcReadme'."
}
if (-not (Test-Path -LiteralPath $srcVersion -PathType Leaf)) {
    throw "Metadata versi tidak ditemukan: '$srcVersion'."
}
$versionLines = Get-Content -LiteralPath $srcVersion
if ($versionLines -notcontains "VERSION=$targetVersion") {
    throw "Metadata VERSION.txt tidak sesuai dengan versi target '$targetVersion'."
}

$sourceHash = (Get-FileHash -LiteralPath $srcExe -Algorithm SHA256).Hash
Write-Host "Preflight executable berhasil." -ForegroundColor Green
Write-Host "Sumber executable : $srcExe"
Write-Host "File Version       : $($versionInfo.FileVersion)"
Write-Host "Product Version    : $($versionInfo.ProductVersion)"
Write-Host "SHA-256 sumber     : $sourceHash"

if (Test-Path -LiteralPath $releaseDir -PathType Leaf) {
    throw "Target release bukan direktori: '$releaseDir'."
}
$existingOutputs = @($pkgDir, $zipPath, $checksumPath) | Where-Object { Test-Path -LiteralPath $_ }
if ($existingOutputs.Count -gt 0) {
    throw "Packaging dihentikan agar tidak menimpa output yang sudah ada: $($existingOutputs -join ', ')."
}

# 1. Siapkan struktur direktori paket
Write-Host "Menyiapkan direktori rilis portable: $pkgDir..."
New-Item -ItemType Directory -Path (Join-Path $pkgDir "data") | Out-Null
New-Item -ItemType Directory -Path (Join-Path $pkgDir "backups") | Out-Null
New-Item -ItemType Directory -Path (Join-Path $pkgDir "exports") | Out-Null

# 2. Salin biner dan dokumentasi rilis
Write-Host "Menyalin executable dan berkas panduan rilis..."
Copy-Item -LiteralPath $srcExe -Destination $dstExe
Copy-Item -LiteralPath $srcReadme -Destination (Join-Path $pkgDir "README.txt")
Copy-Item -LiteralPath $srcVersion -Destination (Join-Path $pkgDir "VERSION.txt")

# 3. Kompres paket rilis ke arsip ZIP
Write-Host "Mengompres paket rilis ke format ZIP..."
Compress-Archive -Path "$pkgDir\*" -DestinationPath $zipPath

# 4. Hitung dan catat checksum SHA-256
Write-Host "Menghitung checksum SHA-256..."
$exeHash = (Get-FileHash -LiteralPath $dstExe -Algorithm SHA256).Hash
$zipHash = (Get-FileHash -LiteralPath $zipPath -Algorithm SHA256).Hash
if ($exeHash -ne $sourceHash) {
    throw "Checksum executable di dalam paket tidak sama dengan executable sumber."
}

$lines = @(
    "$exeHash *ComplianceWorkspace.exe",
    "$zipHash *ComplianceWorkspace-v1.1.1-Windows-x64.zip"
)
Set-Content -LiteralPath $checksumPath -Value $lines -Encoding utf8

Write-Host "Paket rilis berhasil disiapkan!" -ForegroundColor Green
Write-Host "Executable SHA-256 : $exeHash"
Write-Host "Paket ZIP SHA-256  : $zipHash"
