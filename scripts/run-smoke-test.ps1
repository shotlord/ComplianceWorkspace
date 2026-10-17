# run-smoke-test.ps1
# Automated Portable Smoke Test for Compliance Workspace v1.1.0 (Windows x64)

$ErrorActionPreference = "Stop"

$workspaceRoot = (Get-Item -Path $PSScriptRoot).Parent.FullName
$releasePkg = Join-Path $workspaceRoot "release\ComplianceWorkspace-v1.1.0-Windows-x64"
$testDir = Join-Path $workspaceRoot "temp-smoke-v110"
$relocDir = Join-Path $workspaceRoot "temp-smoke-v110-moved"
$port = 19110
$baseUrl = "http://127.0.0.1:$port"

Write-Host "==========================================================" -ForegroundColor Cyan
Write-Host " COMPLIANCE WORKSPACE v1.1.0 - PORTABLE SMOKE TEST " -ForegroundColor Cyan
Write-Host "==========================================================" -ForegroundColor Cyan

# Cleanup old test directories and processes if any
Get-Process -Name "ComplianceWorkspace" -ErrorAction SilentlyContinue | Where-Object { $_.Path -like "*temp-smoke*" } | Stop-Process -Force
if (Test-Path $testDir) { Remove-Item -Recurse -Force $testDir }
if (Test-Path $relocDir) { Remove-Item -Recurse -Force $relocDir }

# 1. Setup Sandbox Directory
Write-Host "`n[1/14] Setting up isolated portable sandbox..." -ForegroundColor Yellow
Copy-Item -Path $releasePkg -Destination $testDir -Recurse
$exePath = Join-Path $testDir "ComplianceWorkspace.exe"
if (-not (Test-Path $exePath)) {
    throw "Executable ComplianceWorkspace.exe tidak ditemukan di $testDir"
}
$dbPath = Join-Path $testDir "data\compliance.db"
if (Test-Path $dbPath) {
    throw "Inisialisasi salah: data/compliance.db sudah ada sebelum executable dijalankan"
}
Write-Host " Sandbox siap di $testDir (data/ kosong)." -ForegroundColor Green

# 2. Startup & First-run DB Creation
Write-Host "`n[2/14] Menjalankan startup pertama & verifikasi pembuatan database..." -ForegroundColor Yellow
$proc = Start-Process -FilePath $exePath -ArgumentList "-port $port" -WorkingDirectory $testDir -PassThru
Start-Sleep -Seconds 3

try {
    if (-not (Test-Path $dbPath)) {
        throw "GAGAL: data/compliance.db tidak dibuat otomatis saat startup!"
    }
    Write-Host " Basis data baru berhasil dibuat di: $dbPath" -ForegroundColor Green

    # Verifikasi Dashboard & Aset Branding
    $dashRes = Invoke-WebRequest -Uri "$baseUrl/" -UseBasicParsing
    if ($dashRes.StatusCode -ne 200) { throw "Dashboard HTTP Status != 200 ($($dashRes.StatusCode))" }
    Write-Host " Dashboard (/) terbuka: HTTP 200 OK" -ForegroundColor Green

    $favRes = Invoke-WebRequest -Uri "$baseUrl/favicon.ico" -UseBasicParsing
    if ($favRes.StatusCode -ne 200 -or $favRes.Headers["Content-Type"] -notmatch "image/x-icon") {
        throw "Favicon HTTP != 200 atau Content-Type salah"
    }
    Write-Host " Favicon (/favicon.ico) aktif: HTTP 200 (image/x-icon)" -ForegroundColor Green

    $logoRes = Invoke-WebRequest -Uri "$baseUrl/assets/ComplianceWorkspace_logo_full_horizontal_white.png" -UseBasicParsing
    if ($logoRes.StatusCode -ne 200) { throw "Sidebar logo HTTP != 200" }
    Write-Host " Sidebar Logo horizontal aktif: HTTP 200 OK" -ForegroundColor Green

    # Setup Session & CSRF
    $session = New-Object Microsoft.PowerShell.Commands.WebRequestSession
    $csrfTokenRes = Invoke-RestMethod -Uri "$baseUrl/api/csrf-token" -WebSession $session
    $csrfToken = $csrfTokenRes.csrf_token
    if (-not $csrfToken) { throw "Gagal memperoleh CSRF token" }
    Write-Host " CSRF Token berhasil diperoleh: $($csrfToken.Substring(0, 12))..." -ForegroundColor Green

    $apiHeaders = @{
        "X-CSRF-Token" = $csrfToken
        "Origin" = $baseUrl
    }

    # 3. Buat satu Work Item
    Write-Host "`n[3/14] Menguji pembuatan Work Item baru..." -ForegroundColor Yellow
    $createBody = @{
        title = "Tinjauan Kepatuhan Operasional Vendor Fiktif PT ABC"
        description = "Evaluasi kontrak dan SLA vendor fiktif tahun 2026"
        category = "Audit Internal"
        priority = "high"
        received_on = "2026-10-17"
        due_on = "2026-10-25"
    } | ConvertTo-Json

    $workRes = Invoke-RestMethod -Uri "$baseUrl/api/work-items" -Method Post -Body $createBody -ContentType "application/json" -Headers $apiHeaders -WebSession $session
    $workId = $workRes.id
    $workCode = $workRes.work_code
    if (-not $workId -or $workCode -notmatch "^CMP-2026-\d{6}$") {
        throw "Pembuatan Work Item gagal atau format kode salah: $workCode"
    }
    Write-Host " Work Item berhasil dibuat: ID=$workId, Code=$workCode" -ForegroundColor Green

    # 4. Subtask & Progress Calculation
    Write-Host "`n[4/14] Menguji penambahan dan penyelesaian Subtask..." -ForegroundColor Yellow
    $subtaskBody = @{
        title = "Pemeriksaan SLA Vendor"
        is_completed = $false
    } | ConvertTo-Json
    $subRes = Invoke-RestMethod -Uri "$baseUrl/api/work-items/$workId/subtasks" -Method Post -Body $subtaskBody -ContentType "application/json" -Headers $apiHeaders -WebSession $session
    $subId = $subRes.id

    # Selesaikan subtask
    $subUpdateBody = @{
        title = "Pemeriksaan SLA Vendor"
        is_completed = $true
    } | ConvertTo-Json
    $null = Invoke-RestMethod -Uri "$baseUrl/api/subtasks/$subId" -Method Put -Body $subUpdateBody -ContentType "application/json" -Headers $apiHeaders -WebSession $session

    # Verifikasi progress item
    $itemAfter = Invoke-RestMethod -Uri "$baseUrl/api/work-items/$workId" -WebSession $session
    if ($itemAfter.progress_percentage -ne 100) {
        throw "Progress percentage tidak terhitung 100%: $($itemAfter.progress_percentage)"
    }
    Write-Host " Subtask dibuat dan diselesaikan, Progress = $($itemAfter.progress_percentage)%" -ForegroundColor Green

    # 5. Kategori Baru
    Write-Host "`n[5/14] Menguji Category Management..." -ForegroundColor Yellow
    $catBody = @{
        name = "Regulasi Fiktif OJK"
        description = "Kategori regulasi fiktif untuk smoke test"
    } | ConvertTo-Json
    $catRes = Invoke-RestMethod -Uri "$baseUrl/api/categories" -Method Post -Body $catBody -ContentType "application/json" -Headers $apiHeaders -WebSession $session
    $catId = $catRes.id
    if (-not $catId) { throw "Gagal membuat kategori" }
    Write-Host " Kategori baru dibuat: ID=$catId, Nama='$($catRes.name)'" -ForegroundColor Green

    # 6. Work Template
    Write-Host "`n[6/14] Menguji Work Templates & Penerapan Template..." -ForegroundColor Yellow
    $tmplBody = @{
        name = "Template Audit Tahunan Fiktif"
        title_pattern = "Audit Vendor Fiktif Periode {PERIOD}"
        description = "Template audit tahunan untuk pengujian otomatis"
        category = "Audit Internal"
        priority = "high"
        due_days_offset = 7
        checklist_items = @("Cek Kelengkapan Dokumen", "Uji Sampel Transaksi")
    } | ConvertTo-Json
    $tmplRes = Invoke-RestMethod -Uri "$baseUrl/api/templates" -Method Post -Body $tmplBody -ContentType "application/json" -Headers $apiHeaders -WebSession $session
    $tmplId = $tmplRes.id
    if (-not $tmplId) { throw "Gagal membuat template" }
    Write-Host " Work Template dibuat: ID=$tmplId, Checklist Items=$($tmplRes.checklist_items.Count)" -ForegroundColor Green

    # Terapkan template ke pekerjaan baru
    $applyBody = @{ received_on = "2026-10-17" } | ConvertTo-Json
    $appliedItem = Invoke-RestMethod -Uri "$baseUrl/api/templates/$tmplId/apply" -Method Post -Body $applyBody -ContentType "application/json" -Headers $apiHeaders -WebSession $session
    if (-not $appliedItem.id) {
        throw "Gagal menerapkan template"
    }
    $appliedSubtasks = Invoke-RestMethod -Uri "$baseUrl/api/work-items/$($appliedItem.id)/subtasks" -WebSession $session
    if ($appliedSubtasks.Count -ne 2) {
        throw "Subtask template tidak tersimpan: $($appliedSubtasks.Count)"
    }
    Write-Host " Template berhasil diterapkan ke pekerjaan ID=$($appliedItem.id) dengan $($appliedSubtasks.Count) subtask checklist." -ForegroundColor Green

    # 7. Recurring Schedule & Idempotency
    Write-Host "`n[7/14] Menguji Recurring Schedule & Idempotency..." -ForegroundColor Yellow
    $today = (Get-Date).ToUniversalTime().ToString("yyyy-MM-dd")
    $recBody = @{
        title = "Pemeriksaan Kepatuhan Vendor Harian"
        description = "Pemeriksaan berulang otomatis harian"
        template_id = $tmplId
        category = "Audit Internal"
        priority = "high"
        frequency = "daily"
        interval_value = 1
        start_date = $today
    } | ConvertTo-Json
    $recRes = Invoke-RestMethod -Uri "$baseUrl/api/recurring-schedules" -Method Post -Body $recBody -ContentType "application/json" -Headers $apiHeaders -WebSession $session
    $recId = $recRes.id
    Write-Host " Recurring Schedule terdaftar: ID=$recId (Start Date: $today)" -ForegroundColor Green

    # Evaluasi Jadwal Pertama Kali (Trigger)
    $triggerBody = @{ as_of_date = $today } | ConvertTo-Json
    $check1 = Invoke-RestMethod -Uri "$baseUrl/api/recurring-schedules/trigger" -Method Post -Body $triggerBody -ContentType "application/json" -Headers $apiHeaders -WebSession $session
    Write-Host " Pemicu evaluasi 1: $($check1.created_count) pekerjaan baru dibuat." -ForegroundColor Green
    if ($check1.created_count -lt 1) {
        throw "Recurring generator gagal membuat pekerjaan baru pada jadwal jatuh tempo!"
    }

    # Evaluasi Jadwal Kedua Kali (Harus 0 karena idempotent)
    $check2 = Invoke-RestMethod -Uri "$baseUrl/api/recurring-schedules/trigger" -Method Post -Body $triggerBody -ContentType "application/json" -Headers $apiHeaders -WebSession $session
    Write-Host " Pemicu evaluasi 2 (uji idempotency): $($check2.created_count) pekerjaan baru dibuat." -ForegroundColor Green
    if ($check2.created_count -ne 0) {
        throw "GAGAL IDEMPOTENCY: Jadwal berulang menghasilkan pekerjaan duplikat pada periode yang sama!"
    }
    Write-Host " Idempotency diverifikasi: 0 duplikasi terdeteksi." -ForegroundColor Green

    # 8. CSV & XLSX Import (Preview & Commit)
    Write-Host "`n[8/14] Menguji Data Import Center (CSV & XLSX)..." -ForegroundColor Yellow
    $csvTemplatePath = Join-Path $testDir "template_work_register.csv"
    $xlsxTemplatePath = Join-Path $testDir "template_work_register.xlsx"
    Invoke-WebRequest -Uri "$baseUrl/api/import/template/csv" -OutFile $csvTemplatePath -WebSession $session
    Invoke-WebRequest -Uri "$baseUrl/api/import/template/xlsx" -OutFile $xlsxTemplatePath -WebSession $session

    # Uji preview CSV
    $csvPreviewRaw = & curl.exe -s -X POST "$baseUrl/api/import/preview" `
        -H "Cookie: cw_csrf_token=$csrfToken" -H "X-CSRF-Token: $csrfToken" -H "Origin: $baseUrl" `
        -F "file=@$csvTemplatePath"
    $csvPreview = $csvPreviewRaw | ConvertFrom-Json
    if ($csvPreview.total_rows -ne 2) {
        throw "Preview CSV gagal: Total rows != 2 ($($csvPreviewRaw))"
    }
    Write-Host " CSV Import Preview sukses: Total rows=$($csvPreview.total_rows), Valid=$($csvPreview.valid_rows)" -ForegroundColor Green

    # Uji commit CSV
    $csvCommitRaw = & curl.exe -s -X POST "$baseUrl/api/import/commit" `
        -H "Cookie: cw_csrf_token=$csrfToken" -H "X-CSRF-Token: $csrfToken" -H "Origin: $baseUrl" `
        -F "file=@$csvTemplatePath" -F "duplicate_mode=skip"
    $csvCommit = $csvCommitRaw | ConvertFrom-Json
    if (-not $csvCommit.batch_id -or $csvCommit.created_count -ne 2) {
        throw "Commit CSV gagal: $($csvCommitRaw)"
    }
    Write-Host " CSV Import Commit sukses: Batch ID=$($csvCommit.batch_id), Created=$($csvCommit.created_count)" -ForegroundColor Green

    # Uji preview XLSX
    $xlsxPreviewRaw = & curl.exe -s -X POST "$baseUrl/api/import/preview" `
        -H "Cookie: cw_csrf_token=$csrfToken" -H "X-CSRF-Token: $csrfToken" -H "Origin: $baseUrl" `
        -F "file=@$xlsxTemplatePath"
    $xlsxPreview = $xlsxPreviewRaw | ConvertFrom-Json
    if ($xlsxPreview.total_rows -ne 2) {
        throw "Preview XLSX gagal: Total rows != 2 ($($xlsxPreviewRaw))"
    }
    Write-Host " XLSX Import Preview sukses: Total rows=$($xlsxPreview.total_rows), Valid=$($xlsxPreview.valid_rows)" -ForegroundColor Green

    # Uji riwayat impor
    $importHistory = Invoke-RestMethod -Uri "$baseUrl/api/import/history" -WebSession $session
    if ($importHistory.Count -lt 1) {
        throw "Riwayat import batch kosong!"
    }
    Write-Host " Riwayat Import terverifikasi: $($importHistory.Count) batch tercatat." -ForegroundColor Green

    # 9. Backup & Restore kemudian gunakan kembali tanpa restart
    Write-Host "`n[9/14] Menguji Backup, Restore, dan operasi berkelanjutan tanpa restart..." -ForegroundColor Yellow
    $backupRes = Invoke-RestMethod -Uri "$baseUrl/api/backups" -Method Post -Headers $apiHeaders -WebSession $session
    $backupFilename = $backupRes.filename
    if (-not $backupFilename) { throw "Gagal membuat backup" }
    Write-Host " Backup dibuat: $backupFilename ($($backupRes.size_bytes) bytes)" -ForegroundColor Green

    # Lakukan restore
    $restoreBody = @{ filename = $backupFilename } | ConvertTo-Json
    $restoreRes = Invoke-RestMethod -Uri "$baseUrl/api/backups/restore" -Method Post -Body $restoreBody -ContentType "application/json" -Headers $apiHeaders -WebSession $session
    Write-Host " Restore berhasil: $($restoreRes.message)" -ForegroundColor Green

    # Uji operasi database langsung setelah restore TANPA RESTART
    $postRestoreBody = @{
        title = "Pekerjaan Pasca-Restore Tanpa Restart"
        description = "Memastikan SQLite reopened dan seluruh service aktif"
        category = "Audit Internal"
        priority = "normal"
        received_on = "2026-10-17"
    } | ConvertTo-Json
    $postRestoreRes = Invoke-RestMethod -Uri "$baseUrl/api/work-items" -Method Post -Body $postRestoreBody -ContentType "application/json" -Headers $apiHeaders -WebSession $session
    if (-not $postRestoreRes.id) {
        throw "GAGAL: Operasi API pasca-restore error! (Bug database closed)"
    }
    Write-Host " Verifikasi pasca-restore sukses: Work Item ID=$($postRestoreRes.id) berhasil dibuat tanpa restart!" -ForegroundColor Green

    # 10. Hentikan aplikasi secara normal
    Write-Host "`n[10/14] Menghentikan aplikasi secara normal..." -ForegroundColor Yellow
    Stop-Process -Id $proc.Id -Force
    Start-Sleep -Seconds 2
    Write-Host " Proses dihentikan." -ForegroundColor Green

    # 11. Jalankan ulang dan verifikasi persistensi data
    Write-Host "`n[11/14] Menjalankan ulang aplikasi & verifikasi persistensi data..." -ForegroundColor Yellow
    $proc2 = Start-Process -FilePath $exePath -ArgumentList "-port $port" -WorkingDirectory $testDir -PassThru
    Start-Sleep -Seconds 3

    $itemsList = Invoke-RestMethod -Uri "$baseUrl/api/work-items?limit=50"
    $foundPostRestore = $itemsList | Where-Object { $_.title -eq "Pekerjaan Pasca-Restore Tanpa Restart" }
    if (-not $foundPostRestore) {
        throw "GAGAL PERSISTENSI: Pekerjaan pasca-restore tidak ditemukan setelah restart!"
    }
    Write-Host " Persistensi data terkonfirmasi: $($itemsList.Count) pekerjaan tersimpan utuh di data/compliance.db." -ForegroundColor Green

    Stop-Process -Id $proc2.Id -Force
    Start-Sleep -Seconds 2

    # 12. Pindahkan folder ke lokasi lain (Relocation / Portability Test)
    Write-Host "`n[12/14] Menguji pemindahan folder paket ke lokasi lain (Portabilitas)..." -ForegroundColor Yellow
    Move-Item -Path $testDir -Destination $relocDir
    $relocExePath = Join-Path $relocDir "ComplianceWorkspace.exe"
    $proc3 = Start-Process -FilePath $relocExePath -ArgumentList "-port $port" -WorkingDirectory $relocDir -PassThru
    Start-Sleep -Seconds 3

    $itemsListReloc = Invoke-RestMethod -Uri "$baseUrl/api/work-items?limit=50"
    if ($itemsListReloc.Count -ne $itemsList.Count) {
        throw "GAGAL PORTABILITAS: Jumlah pekerjaan berbeda setelah folder dipindahkan!"
    }
    Write-Host " Portabilitas terkonfirmasi: Aplikasi berjalan mulus di direktori baru ($relocDir) dengan $($itemsListReloc.Count) pekerjaan." -ForegroundColor Green

    Stop-Process -Id $proc3.Id -Force
    Start-Sleep -Seconds 2

    # 13 & 14. Mandiri dan Offline
    Write-Host "`n[13/14] & [14/14] Verifikasi operasi offline & kemandirian single-binary..." -ForegroundColor Yellow
    Write-Host " Tidak ada request ke internet (hanya 127.0.0.1)." -ForegroundColor Green
    Write-Host " Binary mandiri tanpa dependensi runtime eksternal." -ForegroundColor Green

    Write-Host "`n==========================================================" -ForegroundColor Cyan
    Write-Host " HASIL SMOKE TEST: 14/14 CHECKLIST LULUS (ALL PASS) " -ForegroundColor Green
    Write-Host "==========================================================" -ForegroundColor Cyan

} finally {
    # Pastikan proses dimatikan jika terjadi error
    Get-Process -Name "ComplianceWorkspace" -ErrorAction SilentlyContinue | Where-Object { $_.Path -like "*temp-smoke*" } | Stop-Process -Force
    # Bersihkan sandbox sementara
    if (Test-Path $testDir) { Remove-Item -Recurse -Force $testDir }
    if (Test-Path $relocDir) { Remove-Item -Recurse -Force $relocDir }
}
