# Panduan Pengujian & Jaminan Kualitas (Testing)

Dokumen ini menjelaskan strategi pengujian, verifikasi statis, pengujian integrasi otomatis, serta prosedur pengujian portabilitas untuk **Compliance Workspace v1.1.0**.

---

## 1. Ikhtisar Strategi Pengujian

Kualitas dan integritas sistem diuji melalui 4 lapisan piramida pengujian otomatis:

1. **Pemeriksaan Sintaksis & Linting**:
   - `node --check` pada seluruh berkas JavaScript frontend (`web/static/js/api.js` dan `app.js`).
   - `go vet ./...` untuk mendeteksi kesalahan konkurensi, format string, dan potensi bug pada kode Go.
2. **Unit & Domain Tests**:
   - Validasi entitas model domain, aturan bisnis nomor penugasan `CMP-YYYY-NNNNNN`, kalkulasi persentase progres, dan kepatuhan konstanta status/prioritas.
3. **Integration & Regression Tests**:
   - Pengujian transaksi atomik, integritas SQLite, trigger audit log append-only, sistem migrasi skema 001–006, impor spreadsheet (CSV & XLSX), manajemen tugas, pengarsipan, hingga pemulihan database aktif tanpa restart.
4. **Portable Smoke Test End-to-End (14 Skenario)**:
   - Pengujian otomatis biner hasil rilis pada direktori sandbox terisolasi, menguji daur hidup operasional, persistensi, dan pemindahan direktori portabel.

---

## 2. Menjalankan Pengujian Otomatis

### A. Pemeriksaan Sintaksis JavaScript
```powershell
node --check .\web\static\js\api.js
node --check .\web\static\js\app.js
```

### B. Analisis Statis Go (Linting)
```powershell
go vet ./...
```

### C. Menjalankan Seluruh Unit & Integration Tests
Untuk menjalankan seluruh suite pengujian Go secara bersih (tanpa menggunakan cache hasil uji sebelumnya):

```powershell
go test -count=1 ./...
```

Untuk melihat rincian setiap kasus uji (*verbose mode*):
```powershell
go test -v -count=1 ./tests/...
```

### D. Menjalankan Rangkaian Uji Spesifik
Contoh menjalankan pengujian modul tertentu:
```powershell
# Uji modul impor dan pemetaan kolom
go test -v -count=1 ./tests/... -run TestImport

# Uji idempotensi dan recurring tasks
go test -v -count=1 ./tests/... -run TestTaskManagement

# Uji siklus hidup backup dan pemulihan
go test -v -count=1 ./tests/... -run TestBackupRestore

# Uji regresi simplifikasi modal (Batch 09)
go test -v -count=1 ./tests/... -run TestBatch09
```

---

## 3. Pengujian Portabilitas Otomatis (Portable Smoke Test)

Pengujian *smoke test* memverifikasi biner rilis portabel secara menyeluruh di lingkungan terisolasi (*sandbox*):

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\run-smoke-test.ps1
```

### 14 Skenario Pengujian yang Diverifikasi Otomatis:
1. **Penyusunan Sandbox**: Menyiapkan direktori terisolasi `temp-smoke-v110/` tanpa database awal.
2. **Inisialisasi Pertama**: Startup otomatis, pembuatan `data/compliance.db`, migrasi skema, dan verifikasi aset branding (HTTP 200 pada `/`, `/favicon.ico`, dan logo sidebar).
3. **Pembuatan Work Item**: Pembuatan pekerjaan baru dan penomoran otomatis `CMP-YYYY-NNNNNN`.
4. **Manajemen Subtask**: Penambahan checklist dan kalkulasi matematis progres 100%.
5. **Master Kategori**: Pembuatan kategori baru dengan kode warna kustom.
6. **Work Templates**: Pembuatan template dan penerapan instan ke pekerjaan kepatuhan.
7. **Recurring Schedule & Idempotency**: Evaluasi jadwal berulang dan jaminan anti-duplikasi pada periode yang sama.
8. **Data Import Center**: Validasi format CSV & Excel (XLSX), dry-run preview, dan komit batch atomik.
9. **Backup & Pemulihan Runtime**: Pembuatan snapshot `VACUUM INTO`, restore database, dan verifikasi operasional langsung tanpa mematikan aplikasi.
10. **Graceful Shutdown**: Penghentian aman proses server dengan pelepasan kunci SQLite.
11. **Persistensi Data**: Menjalankan ulang aplikasi dan memvalidasi keutuhan seluruh rekaman yang tersimpan.
12. **Portabilitas & Relokasi**: Memindahkan seluruh direktori aplikasi ke lokasi baru (`temp-smoke-v110-moved`) dan memverifikasi aplikasi tetap berfungsi normal tanpa perubahan path absolut.
13. **Verifikasi Lingkungan Offline**: Memastikan tidak ada koneksi keluar ke internet (hanya `127.0.0.1`).
14. **Kemandirian Single-Binary**: Memastikan aplikasi berjalan mandiri tanpa dependensi runtime eksternal.

---

## 4. Kebijakan Keamanan & Privasi Data Uji

- **Hanya Data Fiktif**: Seluruh skenario pengujian, data contoh CSV/XLSX, dan tes integrasi wajib menggunakan data sampel fiktif.
- **Pembersihan Otomatis**: Folder sandbox pengujian (`temp-smoke-*`) dan basis data sementara otomatis dibersihkan setelah pengujian selesai.
- **Tidak Menyertakan Data Pribadi**: Dilarang menggunakan nama perusahaan riil, nomor identitas asli, dokumen rahasia, maupun kredensial internal dalam repositori kode.
