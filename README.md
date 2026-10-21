# Compliance Workspace v1.1.1

Aplikasi web lokal, mandiri (*portable*), dan *offline-first* yang dirancang khusus untuk **Compliance Officer** guna mengelola pencatatan penugasan kepatuhan (*Work Items*), subtask & checklist, manajemen kategori, template pekerjaan, tugas berulang (*recurring tasks*), pusat impor massal (*Data Import Center*), serta jejak audit aktivitas (*Activity Log*) secara terstruktur, aman, dan tanpa biaya operasional.

---

## Daftar Isi
- [Ikhtisar](#ikhtisar)
- [Fitur Utama v1.1.1](#fitur-utama-v111)
- [Teknologi yang Digunakan](#teknologi-yang-digunakan)
- [Persyaratan Sistem](#persyaratan-sistem)
- [Unduhan Aplikasi (GitHub Releases)](#unduhan-aplikasi-github-releases)
- [Cara Menjalankan Aplikasi](#cara-menjalankan-aplikasi)
- [Kompilasi & Pengujian Ringkas](#kompilasi--pengujian-ringkas)
- [Lokasi Penyimpanan Data & Cadangan](#lokasi-penyimpanan-data--cadangan)
- [Keamanan & Privasi](#keamanan--privasi)
- [Dokumentasi Teknis Terkait](#dokumentasi-teknis-terkait)

---

## Ikhtisar

Compliance Workspace v1.1.1 dibangun untuk menjawab kebutuhan pelacakan aktivitas kepatuhan yang portabel dan dapat dipindahkan antar-laptop tanpa memerlukan instalasi interpreter bahasa (seperti Python), lingkungan Node.js, layanan server database eksternal, maupun ketergantungan pada koneksi internet dan cloud API berbayar.

Seluruh antarmuka web modern disematkan langsung ke dalam biner eksekusi (*single executable binary*) dengan ikon Windows resmi, dan basis data disimpan secara lokal menggunakan mesin SQLite murni dalam folder `data/` yang diresolusi relatif terhadap lokasi biner fisik.

> **Status rilis:** v1.1.1 adalah versi stabil terbaru. Paket Windows x64 tersedia pada [GitHub Releases](https://github.com/shotlord/ComplianceWorkspace/releases/tag/v1.1.1).

---

## Fitur Utama v1.1.1

### 1. Dashboard Ringkasan Kepatuhan
- Metrik visual seketika: Total Pekerjaan, Belum Mulai, Dalam Proses, Menunggu, Selesai, dan Melewati Deadline (*Overdue*).
- Indikator peringatan dini untuk pekerjaan yang membutuhkan perhatian segera.
- Kalkulasi progres rata-rata seluruh pekerjaan kepatuhan aktif.

### 2. Work Register & Task Enhancement
- Manajemen siklus hidup penugasan kepatuhan: Pembuatan, Peninjauan Rincian, Pembaruan Informasi, dan Pengarsipan (*Soft Delete*).
- Kode penugasan unik berurutan otomatis: format **`CMP-YYYY-NNNNNN`** (misal: `CMP-2026-000001`).
- **Subtask & Checklist**: Merinci setiap penugasan ke dalam langkah kerja terukur yang dapat dicentang secara bertahap.
- **Kalkulasi Otomatis Progres**: Persentase progres (`progress_percentage`: 0% - 100%) dihitung otomatis berbasis rasio subtask selesai atau estimasi status pekerjaan.
- **Master Kategori**: Pengelolaan klasifikasi pekerjaan dengan warna visual kustom dan status aktif/non-aktif.
- **Work Templates**: Pembuatan cetak biru penugasan berulang lengkap dengan checklist bawaan dan durasi estimasi.
- **Recurring Tasks**: Penjadwalan pekerjaan rutin otomatis (harian, mingguan, bulanan, kuartalan, tahunan) dengan proteksi *idempotency* anti-duplikasi.
- **Penghapusan Aman Berbasis Audit**: Dukungan penghapusan permanen operasional via penanda *tombstone* (`deleted_at`), membersihkan subtask, dan tetap mengabadikan log audit untuk kebutuhan pengawasan regulator.

### 3. Data Import Center (CSV & Excel XLSX)
- Wizard 4 tahap: Pilih Berkas -> Pemetaan Kolom -> Pratinjau & Validasi (Dry-Run) -> Komit & Ringkasan.
- Mendukung berkas spreadsheet Microsoft Excel (`.xlsx`) dan CSV (dengan/tanpa UTF-8 BOM).
- Pemetaan kolom cerdas (*auto-suggest mapping*) untuk mempermudah migrasi data spreadsheet.
- Validasi read-only pada tahap pratinjau tanpa memodifikasi basis data.
- Deteksi duplikasi ganda: duplikasi dalam berkas yang sama (*in-file*) dan duplikasi terhadap basis data SQLite existing berbasis kombinasi `Judul + Tanggal Diterima + Pemberi Tugas`.
- Snapshot cadangan otomatis (*pre-commit backup safety*) sebelum transaksi impor dikomit.
- Riwayat batch impor dan fasilitas pembatalan kompensasi (*Compensate Rollback*) aman.

### 4. Activity Log (Jejak Audit Kronologis)
- Garis waktu aktivitas (*timeline*) per pekerjaan yang bersifat **append-only** (tidak dapat diubah atau dihapus).
- Pencatatan otomatis oleh sistem setiap kali terjadi pembuatan pekerjaan, perubahan status, penambahan/penyelesaian subtask, impor batch, pengarsipan, atau pemulihan.
- Dilindungi oleh trigger database SQLite `RAISE(ABORT)` untuk mencegah manipulasi data riwayat audit.

### 5. Deadline Tracker (Pelacak Tenggat Waktu)
- Pengelompokan tugas berdasarkan urgensi:
  - **Terlambat (*Overdue*)**: Pekerjaan yang telah melampaui tenggat waktu.
  - **Jatuh Tempo Hari Ini**: Penugasan yang harus diselesaikan pada hari berjalan.
  - **Tujuh Hari Mendatang**: Pekerjaan yang mendekati tenggat dalam 7 hari ke depan.
  - **Tanpa Deadline**: Pekerjaan berkategori fleksibel tanpa tanggal target.

### 6. Settings, Backup & Restore
- Pencadangan konsisten database SQLite menggunakan perintah mesin `VACUUM INTO` ke folder `backups/`.
- Verifikasi integritas otomatis pada setiap berkas cadangan melalui `PRAGMA integrity_check` dan validasi versi skema.
- Pemulihan (*restore*) runtime melalui staging tervalidasi dengan pembuatan salinan pengaman (*safety snapshot*) sebelum database aktif diganti.
- Manajemen siklus hidup berkas cadangan: penghapusan berkas usang secara tunggal maupun massal dengan jaminan retensi minimal 1 berkas cadangan valid.

### 7. Ekspor Laporan Excel (CSV dengan BOM)
- Ekspor seluruh daftar pekerjaan atau log aktivitas ke format CSV standar.
- Disertai **UTF-8 Byte Order Mark (BOM)** agar langsung terbuka rapi pada Microsoft Excel tanpa teks berantakan.
- Proteksi **CSV Formula Injection** untuk nilai teks yang diawali `=`, `+`, `-`, `@`, tab (`\t`), atau carriage return (`\r`).

---

## Teknologi yang Digunakan

- **Bahasa Pemrograman**: Go 1.27.2 (Standard Library `net/http`, `database/sql`, `embed`).
- **Driver Basis Data**: `modernc.org/sqlite` (Pure Go, 100% tanpa CGO / C-Compiler).
- **Pemrosesan Spreadsheet**: `github.com/xuri/excelize/v2` (Pure Go).
- **Frontend**: HTML5, CSS3 murni, dan Vanilla JavaScript (ES6+), disematkan melalui Go `embed`.
- **Ketergantungan Eksternal di Runtime**: **Nol (Zero-dependency)** — tidak membutuhkan CDN, Node.js, Python, atau font daring.

---

## Persyaratan Sistem

- **Sistem Operasi**: Windows 10 atau Windows 11 (64-bit / x64).
- **Hak Akses**: Hak pengguna standar (Non-Administrator dapat menjalankan).
- **Peramban Web**: Microsoft Edge, Google Chrome, Mozilla Firefox, atau peramban modern lainnya.
- **Koneksi Jaringan**: 100% Offline (tidak memerlukan sambungan internet).

---

## Unduhan Aplikasi (GitHub Releases)

Pengguna akhir tidak perlu menginstal Go atau mengompilasi kode program:

1. Buka halaman **Releases** pada repositori GitHub resmi:
   [https://github.com/shotlord/ComplianceWorkspace/releases](https://github.com/shotlord/ComplianceWorkspace/releases)
2. Unduh paket distribusi stabil terbaru (v1.1.1):
   **`ComplianceWorkspace-v1.1.1-Windows-x64.zip`**
3. *(Disarankan)* Unduh berkas **`checksums-sha256.txt`** dan verifikasi keaslian paket di PowerShell:
   ```powershell
   Get-FileHash .\ComplianceWorkspace-v1.1.1-Windows-x64.zip -Algorithm SHA256
   ```
4. Ekstrak berkas ZIP ke direktori lokal pilihan Anda (misalnya `D:\ComplianceWorkspace` atau folder Documents).

---

## Cara Menjalankan Aplikasi

### Metode 1: Menggunakan Executable Rilis Portabel

1. Buka folder hasil ekstrak (`ComplianceWorkspace-v1.1.1-Windows-x64`).
2. Klik ganda pada berkas **`ComplianceWorkspace.exe`**.
3. Konsol akan menampilkan alamat server lokal aktif:
   ```text
   [INFO] Server aktif dan mendengarkan pada: http://127.0.0.1:54321
   ```
4. Buka alamat tersebut secara manual pada peramban web.
5. Untuk menentukan port tertentu melalui terminal:
   ```powershell
   .\ComplianceWorkspace.exe -port 8080
   ```
6. Untuk penutupan tertib (*graceful shutdown*), tekan `Ctrl + C` pada terminal. Menutup paksa proses atau jendela konsol tidak menjamin jalur shutdown berbasis sinyal selesai.

> **Catatan Windows SmartScreen**: Karena aplikasi portabel ini didistribusikan tanpa sertifikat digital komersial (EV Code Signing), Windows Defender SmartScreen mungkin menampilkan layar *"Windows protected your PC"*. Klik **More info** lalu pilih **Run anyway**.

### Metode 2: Menjalankan dari Source Code (Pengembang)

1. Pastikan Go 1.27.2 terpasang pada komputer Anda.
2. Buka terminal pada root direktori repositori.
3. Jalankan perintah:
   ```powershell
   go run ./cmd/compliance-workspace
   ```
4. Buka URL `http://127.0.0.1:<port>` yang tertera pada konsol.

---

## Kompilasi & Pengujian Ringkas

Untuk mengompilasi biner mandiri berikon resmi dan menjalankan seluruh pengujian otomatis:

```powershell
# 1. Jalankan unit test dan static checks
go test -count=1 ./...
go vet ./...

# 2. Kompilasi biner Windows x64 dengan ikon resmi
$buildDir = Join-Path $env:TEMP ("ComplianceWorkspace-v1.1.1-build-" + (Get-Date -Format "yyyyMMdd-HHmmss"))
New-Item -ItemType Directory -Path $buildDir | Out-Null
$executablePath = Join-Path $buildDir "ComplianceWorkspace-v1.1.1-Windows-x64.exe"
powershell -ExecutionPolicy Bypass -File .\scripts\build-windows.ps1 -OutputPath $executablePath

# 3. Setelah source freeze dan otorisasi rilis, buat paket portabel
powershell -ExecutionPolicy Bypass -File .\scripts\package-release.ps1 -ExecutablePath $executablePath

# 4. Jalankan pengujian portabilitas menyeluruh (14 skenario)
powershell -ExecutionPolicy Bypass -File .\scripts\run-smoke-test.ps1
```

`package-release.ps1` tidak boleh dijalankan sebagai packaging resmi sebelum gate rilis memberi otorisasi. Rincian kontrak `-OutputPath`, `-ExecutablePath`, `-SkipTests`, provenance commit, dan batas bukti smoke test dijelaskan pada [docs/BUILDING.md](docs/BUILDING.md) dan [docs/TESTING.md](docs/TESTING.md).

---

## Lokasi Penyimpanan Data & Cadangan

Seluruh berkas operasional disimpan secara lokal relatif terhadap lokasi berkas executable `ComplianceWorkspace.exe`:

- **`data/compliance.db`**: Basis data utama SQLite. Dibuat dan dimigrasikan secara otomatis saat aplikasi pertama kali dijalankan.
- **`backups/*.db`**: Berkas snapshot cadangan konsisten SQLite hasil perintah `VACUUM INTO` dan safety snapshot otomatis.
- **`exports/`**: Folder kosong yang disediakan dalam paket portabel. Ekspor aplikasi dikirim sebagai unduhan HTTP; lokasi akhir file ditentukan oleh pengaturan peramban.

Untuk memindahkan aplikasi ke perangkat laptop lain, cukup matikan aplikasi dan salin seluruh folder aplikasi ke perangkat tujuan.

---

## Keamanan & Privasi

- **Localhost Loopback Only**: Server hanya mengikat koneksi pada `127.0.0.1`. Koneksi dari perangkat lain pada jaringan lokal (LAN) maupun internet otomatis ditolak.
- **Validasi Host & Origin**: Mencegah serangan DNS Rebinding dan manipulasi header request.
- **Proteksi CSRF**: Memvalidasi token kriptografis acak `cw_csrf_token` pada setiap permintaan mutasi data (`POST`, `PUT`, `DELETE`).
- **Content Security Policy (CSP)**: Mengunci eksekusi skrip hanya dari sumber internal lokal (`default-src 'self'`).
- **Tanpa Telemetri**: Aplikasi sama sekali tidak mengumpulkan data penggunaan, analitik, atau berkomunikasi ke server pihak ketiga.

---

## Dokumentasi Teknis Terkait

Untuk pemahaman teknis mendalam mengenai arsitektur, basis data, dan tata kelola rilis:
- **[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)**: Diagram arsitektur, pipeline middleware, spesifikasi endpoint REST API, dan alur data internal.
- **[docs/DATABASE.md](docs/DATABASE.md)**: Skema DDL tabel SQLite, indeks performa, transaksi atomik, migrasi 001–006, dan pola tombstone penghapusan aman.
- **[docs/BUILDING.md](docs/BUILDING.md)**: Prosedur kompilasi biner Windows x64, kompilasi resource ikon `go-winres`, dan packaging rilis.
- **[docs/TESTING.md](docs/TESTING.md)**: Panduan pengujian unit, integrasi, static checks, targeted regression, serta cakupan dan batas portable smoke test.
- **[SECURITY.md](SECURITY.md)**: Model keamanan, batas penggunaan, dan cara melaporkan kerentanan tanpa data sensitif.
- **[CHANGELOG.md](CHANGELOG.md)**: Catatan riwayat perubahan dan peningkatan setiap rilis.
