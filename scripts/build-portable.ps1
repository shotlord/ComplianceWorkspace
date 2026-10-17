<#
.SYNOPSIS
    Build script untuk menghasilkan rilis portable Compliance Workspace v1.0 Windows x64.
.DESCRIPTION
    Skrip ini melakukan kompilasi biner mandiri (CGO_ENABLED=0, GOOS=windows, GOARCH=amd64),
    menyiapkan struktur folder portable release/, menyertakan dokumentasi README.txt dan VERSION.txt,
    membuat arsip ZIP, serta menghitung checksum SHA-256.
.PARAMETER SkipTests
    Lewati pengujian otomatis sebelum build jika disetel ke $true.
#>
param(
    [switch]$SkipTests = $false
)

$ErrorActionPreference = "Stop"

# Tentukan direktori proyek (root)
$ProjectRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
Set-Location $ProjectRoot

Write-Host "==========================================================" -ForegroundColor Cyan
Write-Host " Compliance Workspace v1.0 -- Portable Build (Windows x64) " -ForegroundColor Cyan
Write-Host "==========================================================" -ForegroundColor Cyan
Write-Host "Lokasi Proyek : $ProjectRoot"

# 1. Jalankan pengujian dan linting jika tidak di-skip
if (-not $SkipTests) {
    Write-Host ""
    Write-Host "[1/6] Menjalankan verifikasi kode dan pengujian..." -ForegroundColor Yellow
    
    # Periksa sintaks JavaScript
    if (Get-Command node -ErrorAction SilentlyContinue) {
        Write-Host "  -> Memeriksa sintaks JavaScript..."
        node --check .\web\static\js\app.js
        node --check .\web\static\js\api.js
    } else {
        Write-Host "  -> Node.js tidak ditemukan, lewati node --check"
    }

    # Jalankan go vet
    Write-Host "  -> Menjalankan go vet ./..."
    go vet ./...

    # Jalankan go test
    Write-Host "  -> Menjalankan go test ./..."
    go test ./...
    Write-Host "  -> Seluruh pengujian dan verifikasi berhasil (PASS)." -ForegroundColor Green
} else {
    Write-Host ""
    Write-Host "[1/6] Melewati pengujian otomatis (-SkipTests aktif)." -ForegroundColor Yellow
}

# 2. Siapkan direktori rilis
Write-Host ""
Write-Host "[2/6] Menyiapkan struktur direktori rilis..." -ForegroundColor Yellow
$ReleaseDir = Join-Path $ProjectRoot "release"
$PackageName = "ComplianceWorkspace-v1.0-Windows-x64"
$TargetDir = Join-Path $ReleaseDir $PackageName
$ZipFilePath = Join-Path $ReleaseDir "$PackageName.zip"

if (Test-Path $TargetDir) {
    Write-Host "  -> Menghapus direktori rilis sebelumnya: $TargetDir"
    Remove-Item -Path $TargetDir -Recurse -Force
}
if (Test-Path $ZipFilePath) {
    Write-Host "  -> Menghapus arsip zip sebelumnya: $ZipFilePath"
    Remove-Item -Path $ZipFilePath -Force
}

$DataDir = Join-Path $TargetDir "data"
$BackupsDir = Join-Path $TargetDir "backups"
$ExportsDir = Join-Path $TargetDir "exports"

New-Item -ItemType Directory -Path $DataDir -Force | Out-Null
New-Item -ItemType Directory -Path $BackupsDir -Force | Out-Null
New-Item -ItemType Directory -Path $ExportsDir -Force | Out-Null

Write-Host "  -> Folder struktur berhasil disiapkan di: $TargetDir"

# 3. Kompilasi biner Go Windows AMD64
Write-Host ""
Write-Host "[3/6] Melakukan kompilasi biner Go (CGO_ENABLED=0, GOOS=windows, GOARCH=amd64)..." -ForegroundColor Yellow
$env:CGO_ENABLED = "0"
$env:GOOS = "windows"
$env:GOARCH = "amd64"

$ExePath = Join-Path $TargetDir "ComplianceWorkspace.exe"
go build -trimpath -ldflags "-s -w" -o $ExePath ./cmd/compliance-workspace

if (-not (Test-Path $ExePath)) {
    throw "Gagal menghasilkan executable di: $ExePath"
}
$ExeItem = Get-Item $ExePath
$ExeSizeMB = [Math]::Round($ExeItem.Length / 1MB, 2)
Write-Host "  -> Biner berhasil dikompilasi: $($ExeItem.Name) ($ExeSizeMB MB)" -ForegroundColor Green

# 4. Tulis file VERSION.txt dan salin README.txt
Write-Host ""
Write-Host "[4/6] Menyiapkan file VERSION.txt dan README.txt..." -ForegroundColor Yellow

$BuildTimestampUtc = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
$BuildTimestampWib = (Get-Date).ToString("yyyy-MM-dd HH:mm:ss") + " WIB"
$GoVersionOutput = (go version).Trim()

$VersionLines = @(
    "================================================================================",
    "Compliance Workspace - Informasi Versi & Rilis",
    "================================================================================",
    "Nama Aplikasi        : Compliance Workspace",
    "Versi                : v1.0.0",
    "Arsitektur Target    : Windows x64 (AMD64)",
    "Kompiler             : $GoVersionOutput",
    "Mode Kompilasi       : Pure Go (CGO_ENABLED=0, modernc.org/sqlite)",
    "Waktu Build (WIB)    : $BuildTimestampWib",
    "Waktu Build (UTC)    : $BuildTimestampUtc",
    "Lokasi Data          : Relatif terhadap direktori executable (./data/compliance.db)",
    "Mode Jaringan        : Localhost only (127.0.0.1), Offline-first, Zero Telemetry",
    "================================================================================",
    "Aplikasi ini dibangun secara portabel tanpa dependensi runtime eksternal.",
    "Tidak memerlukan instalasi Python, Node.js, atau database server tambahan.",
    "================================================================================"
)
$VersionPath = Join-Path $TargetDir "VERSION.txt"
[System.IO.File]::WriteAllLines($VersionPath, $VersionLines, [System.Text.Encoding]::UTF8)

# Salin README.txt dari scripts/README.txt
$SrcReadmePath = Join-Path $PSScriptRoot "README.txt"
$DestReadmePath = Join-Path $TargetDir "README.txt"
Copy-Item -Path $SrcReadmePath -Destination $DestReadmePath -Force

Write-Host "  -> VERSION.txt dan README.txt berhasil disertakan."

# 5. Buat Arsip ZIP
Write-Host ""
Write-Host "[5/6] Mengompresi paket rilis ke format ZIP..." -ForegroundColor Yellow
Compress-Archive -Path (Join-Path $TargetDir "*") -DestinationPath $ZipFilePath -Force

$ZipItem = Get-Item $ZipFilePath
$ZipSizeMB = [Math]::Round($ZipItem.Length / 1MB, 2)
Write-Host "  -> Arsip ZIP berhasil dibuat: $($ZipItem.Name) ($ZipSizeMB MB)" -ForegroundColor Green

# 6. Hitung Checksum SHA-256
Write-Host ""
Write-Host "[6/6] Menghitung checksum SHA-256..." -ForegroundColor Yellow
$ExeHash = (Get-FileHash -Path $ExePath -Algorithm SHA256).Hash
$ZipHash = (Get-FileHash -Path $ZipFilePath -Algorithm SHA256).Hash

$ChecksumLines = @(
    "$ExeHash  ComplianceWorkspace.exe",
    "$ZipHash  $PackageName.zip"
)
$ChecksumFilePath = Join-Path $ReleaseDir "checksums-sha256.txt"
[System.IO.File]::WriteAllLines($ChecksumFilePath, $ChecksumLines, [System.Text.Encoding]::UTF8)

Write-Host ""
Write-Host "==========================================================" -ForegroundColor Green
Write-Host " PORTABLE BUILD WINDOWS x64 SELESAI DENGAN SUKSES!        " -ForegroundColor Green
Write-Host "==========================================================" -ForegroundColor Green
Write-Host "Folder Output    : $TargetDir"
Write-Host "File Executable  : $ExePath"
Write-Host "Ukuran Biner     : $ExeSizeMB MB ($($ExeItem.Length) bytes)"
Write-Host "SHA-256 (Exe)    : $ExeHash"
Write-Host "File Arsip ZIP   : $ZipFilePath"
Write-Host "Ukuran ZIP       : $ZipSizeMB MB ($($ZipItem.Length) bytes)"
Write-Host "SHA-256 (Zip)    : $ZipHash"
Write-Host "File Checksum    : $ChecksumFilePath"
Write-Host "==========================================================" -ForegroundColor Green
