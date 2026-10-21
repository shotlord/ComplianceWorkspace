# Panduan Pengujian & Jaminan Kualitas (Testing)

Dokumen ini memetakan pemeriksaan yang tersedia untuk **Compliance Workspace v1.1.1**. Semua test harus memakai data fiktif dan direktori temporer; jangan gunakan database UAT, pengguna, atau backup operasional.

---

## 1. Lapisan Verifikasi

1. **Automated tests**: test Go, pemeriksaan sintaks JavaScript/PowerShell, dan regresi script packaging.
2. **Internal QA**: eksekusi automated tests oleh developer, static review, serta pencatatan output, exit code, dan skip.
3. **Independent QA**: audit dan eksekusi ulang yang terpisah dari bukti developer.
4. **Manual UAT**: pemeriksaan alur pengguna dan tampilan nyata oleh Project Owner. Kompilasi, unit test, atau pemeriksaan HTTP tidak membuktikan hasil browser/UAT.

---

## 2. Quality Gate Umum

Jalankan dari root repository:

```powershell
node --check .\web\static\js\api.js
node --check .\web\static\js\app.js
go vet ./...
go test -count=1 ./...
$goFiles = Get-ChildItem .\cmd, .\internal, .\tests, .\web -Recurse -Filter *.go | Select-Object -ExpandProperty FullName
gofmt -l $goFiles
git diff --check
```

`gofmt -l` hanya menampilkan file Go yang belum terformat; output kosong berarti bersih. Catat exit code aktual setiap perintah. Node.js bersifat prasyarat untuk `node --check`, bukan dependency runtime aplikasi.

Frontend embedded diuji bersama serving aset, sedangkan sintaks JavaScript juga diperiksa langsung:

```powershell
go test -v -count=1 ./tests -run '^Test(FrontendEmbeddedAssetsServing|BrandingAssetsServing|JavaScriptSyntaxValidation)$'
```

---

## 3. Targeted Regression v1.1.1

### A. Security Hardening HTTP dan Frontend

```powershell
go test -v -count=1 ./tests -run '^Test(SecurityHardening|SecurityMiddlewareRejections|OfflineNoExternalCDNOrAnalytics|RejectNonLocalhostBinding|XSSAndSQLInjectionSafety|FrontendCodeNoUnsafeInnerHTML)'
```

Cakupan meliputi exact Host/Origin/Referer, CSRF, CSP, body limits, multipart cleanup, loopback binding, referensi eksternal pada aset, dan jalur XSS/SQL injection yang diuji.

### B. Backup, Structural Validation, Fault Injection, dan Operation Gate

```powershell
go test -v -count=1 ./internal/service -run '^Test(CreateBackupRejectsCorruptedSnapshotFault|RestoreAbortsWhenSafetySnapshotFails|RestoreRollsBackAfterActivationFault|RestoreRejectsBackupSymlink|RestoreOperationGateBlocksConcurrentDatabaseWork|RestoreRejectsStructurallyInvalidBackup|InspectBackupRejectsMigrationHistoryGap|RestoreSupportedLegacyV1BackupMigrates)$'
go test -v -count=1 ./internal/server -run '^TestRecurringTriggerDoesNotRecursivelyAcquireOperationGate$'
go test -v -count=1 ./tests -run '^Test(BackupAndRestore|RestoreReject|BackupLifecycle|RestoreSafetyGuard)'
```

Rangkaian ini memeriksa safety snapshot, rollback aktivasi, validasi struktur dan migration history, restore schema v1 yang didukung, blocking operation gate, serta regresi deadlock recurring/restore dengan timeout terbatas.

### C. Interrupted Restore dan Startup Fail-Closed

```powershell
go test -v -count=1 ./tests -run '^TestCrashRecovery'
```

Kasus mencakup first run, recovery rollback valid, active database yang tidak boleh ditimpa, active database zero-byte/korup/foreign schema, struktur atau migration history tidak valid, schema lama yang didukung, schema di atas v6, artefak staging/rollback, serta performance/timeout pemeriksaan startup.

### D. Windows Symlink, Junction, dan Reparse Point

Jalankan pada Windows native dan periksa output verbose:

```powershell
go test -v -count=1 ./tests -run '^TestCrashRecovery(SymlinkArtifact|WindowsJunctionDirectoryRejected)$'
```

`TestCrashRecoveryWindowsJunctionDirectoryRejected` di-skip pada non-Windows. Pada Windows, tiga subtest—`ParentDirectoryJunction`, `RollbackArtifactJunction`, dan `StagingArtifactJunction`—dapat di-skip jika `mklink /J` tidak diizinkan lingkungan; ketiganya harus terlihat `PASS` tanpa `SKIP` untuk bukti junction lengkap. Test symbolic link juga dapat di-skip bila pembuatan symlink tidak didukung atau memerlukan privilege yang tidak tersedia. Jangan menyamakan static review dengan hasil Windows native.

### E. UAT-111-01 — CSV Mapping dan Duplicate Detection

```powershell
go test -v -count=1 ./tests -run '^Test(ImportWorkRegisterExportDefaultsTitleToJudulAndSkipsExistingDuplicates|SuggestColumnMappingRequiresUnambiguousTitle)$'
```

### F. UAT-111-04 — Deadline Tracker Tujuh Hari

```powershell
go test -v -count=1 ./tests -run '^TestDeadlineTrackerSevenDayCategorization$'
```

### G. UAT-111-05 — UX Writing

```powershell
go test -v -count=1 ./tests -run '^TestUXWritingConsistency$'
node --check .\web\static\js\app.js
node --check .\web\static\js\api.js
```

Test string menjaga istilah dan referensi tombol penting. Kualitas bahasa dan tampilan keseluruhan tetap memerlukan review manusia dan Manual UAT.

### H. REL-111-01 — Packaging Safety

Skrip memerlukan satu executable PE v1.1.1 valid dan satu executable legacy v1.1.0. Ia menyalin script/dokumen ke repository simulasi di `%TEMP%`, tidak memakai `release/` repository aktif, dan membersihkan sandbox miliknya sendiri:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\test-package-release.ps1 `
  -ValidExecutablePath 'C:\path\to\ComplianceWorkspace-v1.1.1.exe' `
  -LegacyExecutablePath 'C:\path\to\ComplianceWorkspace-v1.1.0.exe'
```

Regresi membuktikan sumber hilang dan v1.1.0 ditolak, preflight gagal tidak mengubah output existing, v1.1.1 dapat dipaketkan pada sandbox, hash sumber/paket sama, checksum executable/ZIP dicatat, dan packaging kedua tidak menimpa output.

Syntax PowerShell dapat diperiksa tanpa menjalankan build atau packaging:

```powershell
$scripts = Get-ChildItem .\scripts\*.ps1
$failed = $false
foreach ($script in $scripts) {
    $tokens = $null
    $errors = $null
    [void][System.Management.Automation.Language.Parser]::ParseFile($script.FullName, [ref]$tokens, [ref]$errors)
    if ($errors.Count -gt 0) { $failed = $true; $errors }
}
if ($failed) { exit 1 }
```

---

## 4. Windows Executable Smoke Test

`scripts/run-smoke-test.ps1` memakai package tetap pada `release/ComplianceWorkspace-v1.1.1-Windows-x64/`. Jalankan hanya terhadap package yang memang diotorisasi; script menghapus dan membuat ulang sandbox repository-local bernama `temp-smoke-v111` dan `temp-smoke-v111-moved`.

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\run-smoke-test.ps1
```

`scripts/smoke-test-portable.ps1` adalah wrapper yang menjalankan script yang sama tanpa parameter; cakupan dan batas buktinya identik.

Script menjalankan 14 label/checkpoint: sandbox fresh start dan aset HTTP, pekerjaan, subtask, kategori, template, recurring/idempotency, impor CSV/XLSX, backup/restore runtime, penghentian proses, restart/persistensi, dan relokasi folder. Beberapa batas bukti perlu dicatat:

- checkpoint 10 menggunakan `Stop-Process -Force`; ini menguji restart setelah process termination, bukan graceful shutdown berbasis `Ctrl+C`/`SIGTERM`;
- checkpoint 13 dan 14 mencetak assertion offline/single-binary tetapi tidak memasang network monitor atau dependency tracer;
- source/static tests memeriksa tidak adanya CDN/analytics dan server loopback, tetapi klaim tidak ada koneksi keluar saat runtime memerlukan observasi jaringan terpisah bila dijadikan acceptance criterion;
- smoke test HTTP tidak menggantikan pemeriksaan tampilan melalui browser.

Untuk skenario startup executable yang lebih terfokus—fresh start, active database valid/korup, interrupted restore, dan artefak recovery—gunakan harness RC/manual yang terisolasi dan catat perintah, data dummy, output, serta exit code. Harness eksternal yang tidak tersimpan di repository harus dilaporkan sebagai bukti manual/developer, bukan automated test repository.

---

## 5. Data Uji dan Interpretasi Hasil

- Gunakan `t.TempDir()`, `%TEMP%`, atau folder sandbox baru dengan database fiktif.
- Jangan menyalin database UAT/produksi atau backup pengguna ke test.
- Laporkan `PASS`, `FAIL`, dan `SKIP` secara terpisah; test function tidak sama dengan jumlah subtest.
- Test yang tidak selesai atau hanya ditinjau statis berstatus `NOT VERIFIED`, bukan `PASS`.
- Jangan menjalankan official packaging untuk sekadar menguji dokumentasi atau source sebelum gate rilis mengizinkannya.
