$ErrorActionPreference = "Stop"

$workspaceRoot = $PSScriptRoot | Split-Path -Parent
$releaseDir = Join-Path $workspaceRoot "release"
$pkgDir = Join-Path $releaseDir "ComplianceWorkspace-v1.1.0-Windows-x64"
$zipPath = Join-Path $releaseDir "ComplianceWorkspace-v1.1.0-Windows-x64.zip"
$srcExe = Join-Path $workspaceRoot "uat\compliance-workspace.exe"
$dstExe = Join-Path $pkgDir "ComplianceWorkspace.exe"
$srcReadme = Join-Path $PSScriptRoot "README.txt"
$srcVersion = Join-Path $workspaceRoot "VERSION.txt"

if (-not (Test-Path $srcExe)) {
    throw "Executable sumber tidak ditemukan di '$srcExe'. Jalankan scripts/build-windows.ps1 terlebih dahulu."
}

# 1. Siapkan struktur direktori paket
Write-Host "Menyiapkan direktori rilis portable: $pkgDir..."
New-Item -ItemType Directory -Path (Join-Path $pkgDir "data") -Force | Out-Null
New-Item -ItemType Directory -Path (Join-Path $pkgDir "backups") -Force | Out-Null
New-Item -ItemType Directory -Path (Join-Path $pkgDir "exports") -Force | Out-Null

# 2. Salin biner dan dokumentasi rilis
Write-Host "Menyalin executable dan berkas panduan rilis..."
Copy-Item $srcExe $dstExe -Force
if (Test-Path $srcReadme) {
    Copy-Item $srcReadme (Join-Path $pkgDir "README.txt") -Force
}
if (Test-Path $srcVersion) {
    Copy-Item $srcVersion (Join-Path $pkgDir "VERSION.txt") -Force
}

# 3. Kompres paket rilis ke arsip ZIP
Write-Host "Mengompres paket rilis ke format ZIP..."
if (Test-Path $zipPath) {
    Remove-Item $zipPath -Force
}
Compress-Archive -Path "$pkgDir\*" -DestinationPath $zipPath -Force

# 4. Hitung dan catat checksum SHA-256
Write-Host "Menghitung checksum SHA-256..."
$exeHash = (Get-FileHash $dstExe -Algorithm SHA256).Hash
$zipHash = (Get-FileHash $zipPath -Algorithm SHA256).Hash

$lines = @(
    "$exeHash *ComplianceWorkspace.exe",
    "$zipHash *ComplianceWorkspace-v1.1.0-Windows-x64.zip"
)
$checksumPath = Join-Path $releaseDir "checksums-sha256.txt"
Set-Content -Path $checksumPath -Value $lines -Encoding utf8

Write-Host "Paket rilis berhasil disiapkan!" -ForegroundColor Green
Write-Host "Executable Hash : $exeHash"
Write-Host "ZIP Hash        : $zipHash"
