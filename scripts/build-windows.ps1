<#
.SYNOPSIS
    Build script otomatis dan reproducible untuk menghasilkan biner Compliance Workspace v1.1.1 Windows x64 dengan embedded resource icon.
.DESCRIPTION
    Skrip ini memverifikasi toolchain Go, aset icon resmi, dan tool go-winres,
    menghasilkan Windows Resource (.syso) pada direktori cmd/compliance-workspace,
    menjalankan regression test, mengompilasi biner Windows x64 mandiri (CGO_ENABLED=0),
    serta memverifikasi metadata PE dan checksum SHA-256.
.PARAMETER OutputPath
    Jalur target berkas executable yang dipilih secara eksplisit.
.PARAMETER SkipTests
    Lewati pengujian otomatis sebelum build jika disetel ke $true.
#>
param(
    [string]$OutputPath,
    [switch]$SkipTests = $false
)

$ErrorActionPreference = "Stop"

if ([string]::IsNullOrWhiteSpace($OutputPath)) {
    throw "Parameter -OutputPath wajib diisi dengan jalur build baru yang terisolasi."
}

# Tentukan direktori proyek (root)
$ProjectRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
Set-Location $ProjectRoot

Write-Host "==========================================================" -ForegroundColor Cyan
Write-Host " Compliance Workspace v1.1.1 -- Windows x64 Branded Build " -ForegroundColor Cyan
Write-Host "==========================================================" -ForegroundColor Cyan
Write-Host "Lokasi Proyek : $ProjectRoot"
Write-Host "Target Output : $OutputPath"

# 1. Memeriksa Go toolchain
Write-Host ""
Write-Host "[1/8] Memeriksa Go toolchain..." -ForegroundColor Yellow
$GoVersion = & go version 2>&1
if ($LASTEXITCODE -ne 0) {
    throw "Go toolchain tidak ditemukan atau gagal dijalankan. Pastikan Go terpasang di PATH."
}
Write-Host "  -> Ditemukan: $GoVersion" -ForegroundColor Green

# 2. Memeriksa keberadaan ICO asli
Write-Host ""
Write-Host "[2/8] Memeriksa keberadaan aset ICO resmi..." -ForegroundColor Yellow
$IcoPath = Join-Path $ProjectRoot "assets/branding/ComplianceWorkspace_app.ico"
$ZipPath = Join-Path $ProjectRoot "assets/branding/ComplianceWorkspace_logo.zip"

if (-not (Test-Path $IcoPath)) {
    if (Test-Path $ZipPath) {
        Write-Host "  -> File ICO belum diekstrak. Mengekstrak dari $ZipPath..."
        Add-Type -AssemblyName System.IO.Compression.FileSystem
        $zip = [System.IO.Compression.ZipFile]::OpenRead($ZipPath)
        $icoEntry = $zip.GetEntry("ComplianceWorkspace_Branding_Package_v1.0/04-windows-app/ComplianceWorkspace_app.ico")
        if ($null -eq $icoEntry) {
            $zip.Dispose()
            throw "File ComplianceWorkspace_app.ico tidak ditemukan dalam arsip $ZipPath."
        }
        [System.IO.Compression.ZipFileExtensions]::ExtractToFile($icoEntry, $IcoPath, $true)
        $zip.Dispose()
        Write-Host "  -> Berhasil mengekstrak ICO ke: $IcoPath" -ForegroundColor Green
    } else {
        throw "File ICO sumber tidak ditemukan di: $IcoPath dan arsip $ZipPath tidak tersedia."
    }
} else {
    $icoItem = Get-Item $IcoPath
    Write-Host "  -> File ICO terverifikasi: $($icoItem.Name) ($($icoItem.Length) bytes)" -ForegroundColor Green
}

# 3. Memeriksa keberadaan go-winres
Write-Host ""
Write-Host "[3/8] Memeriksa ketersediaan tool go-winres..." -ForegroundColor Yellow
$WinResCmd = Get-Command "go-winres" -ErrorAction SilentlyContinue
$WinResExe = ""

if ($WinResCmd) {
    $WinResExe = $WinResCmd.Source
} else {
    $GoPath = & go env GOPATH
    $Candidate = Join-Path $GoPath "bin\go-winres.exe"
    if (Test-Path $Candidate) {
        $WinResExe = $Candidate
    } else {
        Write-Host "  -> go-winres tidak ditemukan. Memasang via 'go install github.com/tc-hib/go-winres@v0.3.3'..."
        & go install github.com/tc-hib/go-winres@v0.3.3
        if ($LASTEXITCODE -ne 0 -or -not (Test-Path $Candidate)) {
            throw "Gagal memasang tool go-winres."
        }
        $WinResExe = $Candidate
    }
}
Write-Host "  -> go-winres aktif: $WinResExe" -ForegroundColor Green

# 4. Membuat Windows resource untuk amd64 pada package main
Write-Host ""
Write-Host "[4/8] Menghasilkan Windows Resource (.syso) untuk AMD64..." -ForegroundColor Yellow
$CmdDir = Join-Path $ProjectRoot "cmd/compliance-workspace"
Push-Location $CmdDir

& $WinResExe simply `
  --arch amd64 `
  --icon "../../assets/branding/ComplianceWorkspace_app.ico" `
  --manifest cli `
  --file-description "Compliance Workspace - Offline Compliance Management" `
  --product-name "Compliance Workspace" `
  --file-version "1.1.1.0" `
  --product-version "1.1.1.0" `
  --original-filename "ComplianceWorkspace.exe"

$resExit = $LASTEXITCODE
Pop-Location

if ($resExit -ne 0) {
    throw "Gagal mengompilasi Windows resource (.syso)."
}

# 5. Memastikan file .syso berada pada package main
Write-Host ""
Write-Host "[5/8] Memverifikasi lokasi file .syso..." -ForegroundColor Yellow
$SysoPath = Join-Path $ProjectRoot "cmd/compliance-workspace/rsrc_windows_amd64.syso"
if (-not (Test-Path $SysoPath)) {
    throw "File resource .syso tidak ditemukan pada package main: $SysoPath"
}
$sysoItem = Get-Item $SysoPath
Write-Host "  -> Resource terverifikasi: $($sysoItem.FullName) ($($sysoItem.Length) bytes)" -ForegroundColor Green

# 6. Menjalankan pengujian dan verifikasi kode (jika tidak di-skip)
if (-not $SkipTests) {
    Write-Host ""
    Write-Host "[6/8] Menjalankan verifikasi kode dan regression tests..." -ForegroundColor Yellow

    if (Get-Command node -ErrorAction SilentlyContinue) {
        Write-Host "  -> Memeriksa sintaks JavaScript..."
        node --check .\web\static\js\api.js
        node --check .\web\static\js\app.js
    }

    Write-Host "  -> Menjalankan go vet ./..."
    & go vet ./...
    if ($LASTEXITCODE -ne 0) {
        throw "Pemeriksaan go vet gagal."
    }

    Write-Host "  -> Menjalankan go test -count=1 ./..."
    & go test -count=1 ./...
    if ($LASTEXITCODE -ne 0) {
        throw "Pengujian go test gagal."
    }
    Write-Host "  -> Seluruh pengujian dan verifikasi berhasil (PASS)." -ForegroundColor Green
} else {
    Write-Host ""
    Write-Host "[6/8] Melewati pengujian otomatis (-SkipTests aktif)." -ForegroundColor Yellow
}

# 7. Mengompilasi biner Windows x64
Write-Host ""
Write-Host "[7/8] Mengompilasi biner Go Windows AMD64 (CGO_ENABLED=0)..." -ForegroundColor Yellow
$env:CGO_ENABLED = "0"
$env:GOOS = "windows"
$env:GOARCH = "amd64"

# Pastikan folder target ada
$TargetParent = Split-Path -Parent $OutputPath
if ($TargetParent -and -not (Test-Path $TargetParent)) {
    New-Item -ItemType Directory -Path $TargetParent -Force | Out-Null
}

& go build -trimpath -ldflags="-s -w" -o $OutputPath ./cmd/compliance-workspace
if ($LASTEXITCODE -ne 0) {
    throw "Kompilasi biner Go gagal."
}

if (-not (Test-Path $OutputPath)) {
    throw "Berkas executable tidak terbentuk di: $OutputPath"
}

$ExeItem = Get-Item $OutputPath
$ExeSizeMB = [Math]::Round($ExeItem.Length / 1MB, 2)
Write-Host "  -> Biner berhasil dikompilasi: $($ExeItem.Name) ($($ExeItem.Length) bytes / $ExeSizeMB MB)" -ForegroundColor Green

# 8. Memeriksa metadata resource executable dan SHA-256
Write-Host ""
Write-Host "[8/8] Memeriksa metadata Windows PE dan menghitung checksum SHA-256..." -ForegroundColor Yellow

$verInfo = [System.Diagnostics.FileVersionInfo]::GetVersionInfo($ExeItem.FullName)
Write-Host "  -> File Description : $($verInfo.FileDescription)"
Write-Host "  -> Product Name     : $($verInfo.ProductName)"
Write-Host "  -> File Version      : $($verInfo.FileVersion)"
Write-Host "  -> Product Version   : $($verInfo.ProductVersion)"

if ($verInfo.ProductName -ne "Compliance Workspace") {
    throw "Verifikasi metadata gagal: ProductName '$($verInfo.ProductName)' tidak sesuai."
}
if ($verInfo.FileVersion -ne "1.1.1.0") {
    throw "Verifikasi metadata gagal: FileVersion '$($verInfo.FileVersion)' tidak sesuai."
}
if ($verInfo.ProductVersion -ne "1.1.1.0") {
    throw "Verifikasi metadata gagal: ProductVersion '$($verInfo.ProductVersion)' tidak sesuai."
}

# Verifikasi Icon shell extraction
Add-Type -AssemblyName System.Drawing
$extractedIcon = [System.Drawing.Icon]::ExtractAssociatedIcon($ExeItem.FullName)
if ($null -eq $extractedIcon) {
    throw "Gagal mengekstrak icon asosiasi Windows Shell dari executable."
}
Write-Host "  -> Windows Shell Icon: Terverifikasi ($($extractedIcon.Width)x$($extractedIcon.Height) px)" -ForegroundColor Green
$extractedIcon.Dispose()

# Hitung Checksum SHA-256
$Hash = (Get-FileHash -Path $ExeItem.FullName -Algorithm SHA256).Hash
Write-Host "  -> Checksum SHA-256  : $Hash" -ForegroundColor Green

Write-Host ""
Write-Host "==========================================================" -ForegroundColor Green
Write-Host " BUILD SELESAI DENGAN SUKSES!                              " -ForegroundColor Green
Write-Host " Executable : $($ExeItem.FullName)" -ForegroundColor Green
Write-Host " SHA-256    : $Hash" -ForegroundColor Green
Write-Host "==========================================================" -ForegroundColor Green
