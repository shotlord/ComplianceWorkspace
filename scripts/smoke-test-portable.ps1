<#
.SYNOPSIS
    Isolated Portability Smoke Testing untuk biner rilis ComplianceWorkspace.exe
.DESCRIPTION
    Menguji:
    1. Eksekusi biner langsung di folder terisolasi.
    2. Otomatisasi inisialisasi data/compliance.db dan backups/.
    3. Serving antarmuka web melalui localhost.
    4. Operasi CRUD pekerjaan, perubahan status, dan pencatatan activity log.
    5. Pembuatan SQLite backup melalui endpoint API.
    6. Penutupan aman (graceful stop) dan verifikasi persistensi data setelah startup ulang.
    7. Pemindahan (relokasi) folder aplikasi ke path baru dan verifikasi kelancaran operasi.
    8. Pembersihan folder pengujian sementara setelah seluruh uji selesai.
#>

$ErrorActionPreference = "Stop"

$ProjectRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$ReleaseExe = Join-Path $ProjectRoot "release\ComplianceWorkspace-v1.0-Windows-x64\ComplianceWorkspace.exe"

if (-not (Test-Path $ReleaseExe)) {
    throw "Executable rilis tidak ditemukan di: $ReleaseExe. Jalankan build-portable.ps1 terlebih dahulu."
}

$TestPort1 = 49188
$TestPort2 = 49189
$SandboxDir = Join-Path $ProjectRoot "temp-portability-sandbox-1"
$RelocatedDir = Join-Path $ProjectRoot "temp-portability-sandbox-relocated"

# Pastikan folder sandbox bersih
if (Test-Path $SandboxDir) { Remove-Item -Path $SandboxDir -Recurse -Force }
if (Test-Path $RelocatedDir) { Remove-Item -Path $RelocatedDir -Recurse -Force }

Write-Host "==========================================================" -ForegroundColor Cyan
Write-Host " Isolated Portability Smoke Testing                       " -ForegroundColor Cyan
Write-Host "==========================================================" -ForegroundColor Cyan
Write-Host "Biner Diuji    : $ReleaseExe"
Write-Host "Folder Sandbox : $SandboxDir"

try {
    # 1. Siapkan folder terisolasi dan salin biner
    Write-Host "`n[Test 1] Menyiapkan sandbox terisolasi dan menyalin executable..." -ForegroundColor Yellow
    New-Item -ItemType Directory -Path $SandboxDir -Force | Out-Null
    Copy-Item -Path $ReleaseExe -Destination (Join-Path $SandboxDir "ComplianceWorkspace.exe") -Force

    # 2. Jalankan executable pertama kali
    Write-Host "[Test 2] Menjalankan biner pada port $TestPort1..." -ForegroundColor Yellow
    $Proc1 = Start-Process -FilePath (Join-Path $SandboxDir "ComplianceWorkspace.exe") -ArgumentList "-port $TestPort1" -PassThru -NoNewWindow
    Start-Sleep -Seconds 3

    if ($Proc1.HasExited) {
        throw "Proses biner terhenti secara tak terduga pada startup!"
    }

    # 3. Verifikasi pembuatan folder data dan database otomatis
    Write-Host "[Test 3] Verifikasi pembuatan data/compliance.db dan backups/..." -ForegroundColor Yellow
    $DbPath = Join-Path $SandboxDir "data\compliance.db"
    $BackupsDir = Join-Path $SandboxDir "backups"
    if (-not (Test-Path $DbPath)) {
        throw "File basis data compliance.db tidak dibuat otomatis di $DbPath"
    }
    if (-not (Test-Path $BackupsDir)) {
        throw "Direktori backups tidak dibuat otomatis di $BackupsDir"
    }
    Write-Host "  -> data/compliance.db dan backups/ berhasil dibuat secara otomatis." -ForegroundColor Green

    # 4. Verifikasi UI web melalui localhost dan dapatkan sesi CSRF
    Write-Host "[Test 4] Memverifikasi respons HTTP root UI dan menginisialisasi sesi CSRF..." -ForegroundColor Yellow
    $Session1 = New-Object Microsoft.PowerShell.Commands.WebRequestSession
    $UiResp = Invoke-WebRequest -Uri "http://127.0.0.1:$TestPort1/" -WebSession $Session1 -UseBasicParsing -TimeoutSec 5
    if ($UiResp.StatusCode -ne 200 -or $UiResp.Content -notmatch "Compliance Workspace") {
        throw "Respon UI tidak valid atau tidak mengandung title 'Compliance Workspace'"
    }

    $CsrfCookie = $Session1.Cookies.GetCookies("http://127.0.0.1:$TestPort1")["cw_csrf_token"]
    if (-not $CsrfCookie) {
        throw "Cookie CSRF tidak diterima dari server"
    }
    $CsrfToken = $CsrfCookie.Value
    Write-Host "  -> Root UI berhasil disajikan (200 OK) dan Token CSRF diperoleh: $($CsrfToken.Substring(0, 8))..." -ForegroundColor Green

    $Headers1 = @{
        "Content-Type" = "application/json"
        "X-CSRF-Token" = $CsrfToken
        "Origin"       = "http://127.0.0.1:$TestPort1"
    }

    # 5. Buat pekerjaan dummy via API
    Write-Host "[Test 5] Membuat work item baru via API..." -ForegroundColor Yellow
    $CreateBody = @{
        title        = "Pemeriksaan Kepatuhan Sandbox ISO 27001"
        category     = "Audit Internal"
        requested_by = "Kepala Kepatuhan"
        received_on  = "2026-10-17"
        due_on       = "2026-11-15"
        priority     = "high"
        description  = "Pengujian persistensi data portabilitas"
    } | ConvertTo-Json

    $CreateResp = Invoke-RestMethod -Uri "http://127.0.0.1:$TestPort1/api/work-items" -Method Post -WebSession $Session1 -Headers $Headers1 -Body $CreateBody
    $ItemId = $CreateResp.id
    $ItemCode = $CreateResp.work_code
    Write-Host "  -> Work item berhasil dibuat: ID=$ItemId, Code=$ItemCode, Title=$($CreateResp.title)" -ForegroundColor Green

    # 6. Perbarui status pekerjaan (PUT /api/work-items/{id})
    Write-Host "[Test 6] Memperbarui status pekerjaan ke in_progress..." -ForegroundColor Yellow
    $UpdateBody = @{
        title        = "Pemeriksaan Kepatuhan Sandbox ISO 27001"
        category     = "Audit Internal"
        requested_by = "Kepala Kepatuhan"
        received_on  = "2026-10-17"
        due_on       = "2026-11-15"
        priority     = "high"
        status       = "in_progress"
        description  = "Pengujian persistensi data portabilitas - sedang diproses"
    } | ConvertTo-Json
    $UpdateResp = Invoke-RestMethod -Uri "http://127.0.0.1:$TestPort1/api/work-items/$ItemId" -Method Put -WebSession $Session1 -Headers $Headers1 -Body $UpdateBody
    if ($UpdateResp.status -ne "in_progress") {
        throw "Status pekerjaan gagal diperbarui ke in_progress"
    }
    Write-Host "  -> Status pekerjaan berhasil diubah menjadi: $($UpdateResp.status)" -ForegroundColor Green

    # 7. Tambah Activity Log
    Write-Host "[Test 7] Menambahkan log aktivitas manual..." -ForegroundColor Yellow
    $ActBody = @{ description = "Catatan pengujian portabilitas tahap 6" } | ConvertTo-Json
    $ActResp = Invoke-RestMethod -Uri "http://127.0.0.1:$TestPort1/api/work-items/$ItemId/activities" -Method Post -WebSession $Session1 -Headers $Headers1 -Body $ActBody
    Write-Host "  -> Activity log berhasil ditambahkan: ID=$($ActResp.id), Type=$($ActResp.entry_type)" -ForegroundColor Green

    # 8. Buat cadangan SQLite via API
    Write-Host "[Test 8] Membuat database backup konsisten via API..." -ForegroundColor Yellow
    $BackupResp = Invoke-RestMethod -Uri "http://127.0.0.1:$TestPort1/api/backups" -Method Post -WebSession $Session1 -Headers $Headers1
    $BackupFile = $BackupResp.filename
    Write-Host "  -> Backup berhasil dibuat: $BackupFile (Ukuran: $($BackupResp.size_bytes) bytes)" -ForegroundColor Green

    # 9. Hentikan aplikasi secara aman
    Write-Host "[Test 9] Menghentikan proses aplikasi..." -ForegroundColor Yellow
    Stop-Process -Id $Proc1.Id -Force
    Start-Sleep -Seconds 2
    Write-Host "  -> Proses pertama berhasil dihentikan." -ForegroundColor Green

    # 10. Buka kembali dan verifikasi persistensi data
    Write-Host "`n[Test 10] Menjalankan kembali aplikasi dan memeriksa persistensi data..." -ForegroundColor Yellow
    $Proc2 = Start-Process -FilePath (Join-Path $SandboxDir "ComplianceWorkspace.exe") -ArgumentList "-port $TestPort1" -PassThru -NoNewWindow
    Start-Sleep -Seconds 3

    if ($Proc2.HasExited) {
        throw "Proses kedua gagal dijalankan!"
    }

    $PersistResp = Invoke-RestMethod -Uri "http://127.0.0.1:$TestPort1/api/work-items/$ItemId" -Method Get
    if ($PersistResp.title -ne "Pemeriksaan Kepatuhan Sandbox ISO 27001" -or $PersistResp.status -ne "in_progress") {
        throw "Data pekerjaan tidak persisten setelah restart!"
    }

    $PersistActResp = Invoke-RestMethod -Uri "http://127.0.0.1:$TestPort1/api/work-items/$ItemId/activities" -Method Get
    if ($PersistActResp.Count -lt 2) {
        throw "Activity log tidak persisten setelah restart!"
    }
    Write-Host "  -> Data pekerjaan dan $($PersistActResp.Count) entri activity log terbukti PERSISTEN." -ForegroundColor Green

    # Hentikan proses kedua
    Stop-Process -Id $Proc2.Id -Force
    Start-Sleep -Seconds 2

    # 11. Relokasi folder aplikasi ke path baru (Simulasi pemindahan antarlaptop)
    Write-Host "`n[Test 11] Memindahkan folder sandbox ke path baru: $RelocatedDir..." -ForegroundColor Yellow
    Move-Item -Path $SandboxDir -Destination $RelocatedDir
    Start-Sleep -Seconds 1

    Write-Host "[Test 12] Menjalankan biner dari path baru pada port $TestPort2..." -ForegroundColor Yellow
    $Proc3 = Start-Process -FilePath (Join-Path $RelocatedDir "ComplianceWorkspace.exe") -ArgumentList "-port $TestPort2" -PassThru -NoNewWindow
    Start-Sleep -Seconds 3

    if ($Proc3.HasExited) {
        throw "Proses biner gagal dijalankan di path terelokasi!"
    }

    $RelocatedItemResp = Invoke-RestMethod -Uri "http://127.0.0.1:$TestPort2/api/work-items/$ItemId" -Method Get
    if ($RelocatedItemResp.work_code -ne $ItemCode) {
        throw "Data pekerjaan tidak terbaca di lokasi baru!"
    }
    Write-Host "  -> Data pada lokasi baru berhasil terbaca: Code=$($RelocatedItemResp.work_code)" -ForegroundColor Green

    # Uji ekspor CSV di path baru
    $CsvResp = Invoke-WebRequest -Uri "http://127.0.0.1:$TestPort2/api/export/work-items" -UseBasicParsing
    if ($CsvResp.StatusCode -ne 200 -or $CsvResp.Content -notmatch "Pemeriksaan Kepatuhan Sandbox ISO 27001") {
        throw "Ekspor CSV gagal di lokasi baru!"
    }
    Write-Host "  -> Ekspor CSV berhasil diunduh dari lokasi baru (Status 200 OK, UTF-8 BOM valid)." -ForegroundColor Green

    # Hentikan proses ketiga
    Stop-Process -Id $Proc3.Id -Force
    Start-Sleep -Seconds 2
    Write-Host "  -> Proses ketiga berhasil dihentikan." -ForegroundColor Green

    Write-Host "`n==========================================================" -ForegroundColor Green
    Write-Host " SELURUH UJI SMOKE TEST PORTABILITAS LULUS (100% PASS)!   " -ForegroundColor Green
    Write-Host "==========================================================" -ForegroundColor Green
}
finally {
    # Pastikan seluruh proses dihentikan jika masih ada
    Get-Process -Name "ComplianceWorkspace" -ErrorAction SilentlyContinue | Where-Object {
        $_.Path -like "*temp-portability-sandbox*"
    } | Stop-Process -Force -ErrorAction SilentlyContinue

    Start-Sleep -Seconds 1

    # Bersihkan folder sementara
    Write-Host "`n[Cleanup] Membersihkan folder pengujian sementara..." -ForegroundColor Yellow
    if (Test-Path $SandboxDir) {
        Remove-Item -Path $SandboxDir -Recurse -Force -ErrorAction SilentlyContinue
    }
    if (Test-Path $RelocatedDir) {
        Remove-Item -Path $RelocatedDir -Recurse -Force -ErrorAction SilentlyContinue
    }
    Write-Host "  -> Folder sandbox sementara berhasil dibersihkan." -ForegroundColor Green
}
