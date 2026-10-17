# Compliance Workspace v1.0

Aplikasi web lokal, mandiri (*portable*), dan *offline-first* yang dirancang khusus untuk **Compliance Officer** guna mencatat penugasan kepatuhan (*Work Items*) serta jejak audit aktivitas (*Activity Log*) secara terstruktur, aman, dan tanpa biaya operasional.

---

## Daftar Isi
- [Ikhtisar](#ikhtisar)
- [Fitur Utama](#fitur-utama)
- [Teknologi yang Digunakan](#teknologi-yang-digunakan)
- [Persyaratan Sistem](#persyaratan-sistem)
- [Cara Memperoleh Executable (GitHub Releases)](#cara-memperoleh-executable-github-releases)
- [Cara Menjalankan Aplikasi](#cara-menjalankan-aplikasi)
- [Cara Melakukan Build Portabel Mandiri](#cara-melakukan-build-portabel-mandiri)
- [Pencadangan (Backup) & Pemulihan (Restore)](#pencadangan-backup--pemulihan-restore)
- [Ekspor Laporan (CSV Kompatibel Excel)](#ekspor-laporan-csv-kompatibel-excel)
- [Keamanan & Privasi](#keamanan--privasi)
- [Dokumentasi Lanjutan](#dokumentasi-lanjutan)

---

## Ikhtisar

Compliance Workspace v1.0 dibangun untuk menjawab kebutuhan pelacakan aktivitas kepatuhan yang portabel dan dapat dipindahkan antar-laptop tanpa memerlukan instalasi interpreter bahasa (seperti Python), lingkungan Node.js, layanan server database eksternal, maupun ketergantungan pada koneksi internet dan cloud API berbayar.

Seluruh antarmuka web modern telah disematkan langsung ke dalam biner eksekusi (*single executable binary*), dan basis data disimpan secara lokal menggunakan mesin SQLite murni dalam folder `data/` yang diresolusi relatif terhadap lokasi biner fisik.

---

## Fitur Utama

### 1. Dashboard Ringkasan Kepatuhan
- Metrik visual seketika untuk: Total Pekerjaan, Belum Mulai, Dalam Proses, Menunggu, Selesai, dan Melewati Deadline (*Overdue*).
- Indikator peringatan dini untuk pekerjaan yang membutuhkan perhatian segera.

### 2. Work Register (Pencatatan Penugasan)
- Manajemen siklus hidup pekerjaan: Pembuatan, Peninjauan Rincian, Pembaruan Informasi, dan Pengarsipan (*Archive/Soft Delete*).
- Kode penugasan unik berurutan otomatis: format **`CMP-YYYY-NNNNNN`** (misal: `CMP-2026-000001`).
- Atribut lengkap: Judul, Deskripsi, Kategori, Pemberi Tugas, Tanggal Diterima, Batas Waktu (*Due Date*), Prioritas (*Low*, *Normal*, *High*, *Critical*), dan Status.
- Fasilitas pencarian teks bebas instan serta penyaringan multibaris (*multi-filter*) berdasarkan status, kategori, dan prioritas.

### 3. Activity Log (Jejak Audit Kronologis)
- Garis waktu aktivitas (*timeline*) per pekerjaan yang bersifat **append-only** (tidak dapat dihapus atau ditimpa).
- Pencatatan otomatis oleh sistem setiap kali terjadi pembuatan pekerjaan, perubahan status, pengeditan atribut, pengarsipan, atau pemulihan.
- Formulir penambahan catatan progres manual oleh petugas.
- Dilindungi oleh trigger database SQLite `RAISE(ABORT)` untuk mencegah manipulasi data riwayat audit.

### 4. Deadline Tracker (Pelacak Tenggat Waktu)
- Pengelompokan tugas berdasarkan urgensi:
  - **Terlambat (*Overdue*)**: Pekerjaan yang telah melampaui tenggat waktu.
  - **Jatuh Tempo Hari Ini**: Penugasan yang harus diselesaikan pada hari berjalan.
  - **Tujuh Hari Mendatang**: Pekerjaan yang mendekati tenggat dalam 7 hari ke depan.
  - **Tanpa Deadline**: Pekerjaan berkategori fleksibel tanpa tanggal target.
- Pekerjaan yang telah selesai (*completed*) atau dibatalkan (*cancelled*) otomatis dikeluarkan dari penghitungan deadline aktif.

### 5. Settings, Backup & Restore
- Pencadangan konsisten database SQLite menggunakan perintah mesin `VACUUM INTO` ke folder `backups/`.
- Verifikasi integritas otomatis pada setiap berkas cadangan melalui `PRAGMA integrity_check`.
- Pemulihan (*restore*) aman bertingkat yang otomatis membuat salinan darurat (*safety snapshot*) dari database aktif sebelum penggantian data dilakukan.
- Pengaturan preferensi zona waktu tampilan (default: `Asia/Jakarta`).

### 6. Ekspor Data Excel (CSV dengan BOM)
- Ekspor seluruh daftar pekerjaan atau log aktivitas ke format CSV standar.
- Disertai **UTF-8 Byte Order Mark (BOM)** agar langsung terbuka rapi dengan format teks dan karakter non-ASCII yang benar pada Microsoft Excel.
- Proteksi aktif terhadap ancaman **CSV Formula Injection** (`=`, `+`, `-`, `@`, `\t`, `\r`).

---

## Teknologi yang Digunakan

- **Bahasa Pemrograman**: Go 1.22+ (Standard Library `net/http`, `database/sql`, `embed`).
- **Driver Basis Data**: `modernc.org/sqlite` (Pure Go, 100% tanpa CGO / C-Compiler).
- **Frontend**: HTML5, CSS3 murni, dan Vanilla JavaScript (ES6+), disematkan melalui Go `embed`.
- **Ketergantungan Eksternal di Runtime**: **Nol (Zero-dependency)** — tidak membutuhkan CDN, Node.js, Python, atau font daring.

---

## Persyaratan Sistem

- **Sistem Operasi**: Windows 10 atau Windows 11 (64-bit / x64). *(Build Linux & macOS dapat dikompilasi dari sumber)*.
- **Hak Akses**: Hak pengguna standar (Non-Administrator dapat menjalankan).
- **Peramban Web**: Microsoft Edge, Google Chrome, Mozilla Firefox, atau peramban web modern lainnya.
- **Koneksi Jaringan**: 100% Offline (tidak memerlukan sambungan internet).

---

## Cara Memperoleh Executable (GitHub Releases)

Pengguna akhir tidak perlu menginstal Go atau mengompilasi kode program:

1. Buka halaman **Releases** pada repositori GitHub proyek ini:
   `https://github.com/<owner>/compliance-workspace/releases`
2. Temukan rilis stabil terbaru (misalnya `v1.0.0`).
3. Unduh berkas arsip distribusi:
   **`ComplianceWorkspace-v1.0-Windows-x64.zip`**
4. *(Disarankan)* Unduh berkas **`checksums-sha256.txt`** dan verifikasi keaslian biner di PowerShell:
   ```powershell
   Get-FileHash .\ComplianceWorkspace-v1.0-Windows-x64.zip -Algorithm SHA256
   ```
   Pastikan nilai *hash* cocok dengan yang tercantum pada rilis resmi.
5. Ekstrak berkas ZIP ke direktori penyimpanan lokal (misalnya `C:\Users\<Nama>\Documents\ComplianceWorkspace`).

---

## Cara Menjalankan Aplikasi

### Metode 1: Menggunakan Executable Rilis Portabel

1. Buka folder hasil ekstrak (`ComplianceWorkspace-v1.0-Windows-x64`).
2. Klik ganda pada berkas **`ComplianceWorkspace.exe`**.
3. Jendela konsol akan menampilkan informasi server aktif, misalnya:
   ```text
   [INFO] Server aktif dan mendengarkan pada: http://127.0.0.1:54321
   ```
4. Buka peramban web Anda dan akses URL yang tertera tersebut.
5. Untuk menentukan port tertentu, jalankan melalui terminal:
   ```powershell
   .\ComplianceWorkspace.exe -port 8080
   ```
6. Untuk menghentikan aplikasi, tekan `Ctrl + C` pada terminal atau tutup jendela konsol. Aplikasi akan melakukan penutupan tertib (*graceful shutdown*).

> **Catatan Windows SmartScreen**: Karena aplikasi portabel ini didistribusikan tanpa sertifikat digital berbayar (EV Code Signing), Windows Defender SmartScreen mungkin menampilkan layar *"Windows protected your PC"*. Klik **More info** lalu klik tombol **Run anyway**.

### Metode 2: Menjalankan dari Source Code (Pengembang)

1. Pastikan Go 1.22+ telah terinstal pada laptop pengembang.
2. Clone atau buka repositori pada terminal.
3. Jalankan perintah:
   ```powershell
   go run ./cmd/compliance-workspace
   ```
4. Akses alamat `http://127.0.0.1:<port>` yang ditampilkan di log konsol.

---

## Cara Melakukan Build Portabel Mandiri

Tersedia skrip PowerShell otomatis untuk menghasilkan paket distribusi rilis yang bersih:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\build-portable.ps1
```

Skrip ini secara otomatis akan:
1. Memvalidasi sintaks JavaScript dan menjalankan seluruh unit test Go (`go test ./...` dan `go vet ./...`).
2. Mengompilasi biner mandiri Windows AMD64 dengan `CGO_ENABLED=0` dan optimasi `-ldflags "-s -w"`.
3. Menyiapkan folder rilis `release/ComplianceWorkspace-v1.0-Windows-x64/` beserta folder kosong `data/`, `backups/`, dan `exports/`.
4. Menyertakan dokumentasi `README.txt` dan `VERSION.txt`.
5. Mengompresi paket ke `ComplianceWorkspace-v1.0-Windows-x64.zip`.
6. Menghitung dan menyimpan nilai checksum SHA-256 ke `release/checksums-sha256.txt`.

Untuk menguji hasil biner di sandbox terisolasi:
```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\smoke-test-portable.ps1
```

---

## Pencadangan (Backup) & Pemulihan (Restore)

### Melalui Antarmuka Web
1. Masuk ke menu **Pengaturan & Backup** pada bilah navigasi kiri.
2. Klik tombol **Buat Cadangan Baru** untuk membuat snapshot konsisten SQLite.
3. Untuk memulihkan data, pilih salah satu berkas dari daftar riwayat cadangan, lalu klik tombol **Pulihkan**. Sistem secara otomatis membuat salinan darurat dari database yang sedang berjalan sebelum melakukan pemulihan.

### Pencadangan Manual (Folder Relokasi)
Saat aplikasi sedang **tidak berjalan**, Anda dapat menyalin seluruh folder instalasi atau berkas `data/compliance.db` ke media penyimpanan eksternal. Untuk memindahkan ke laptop lain, cukup salin seluruh folder aplikasi ke laptop tujuan dan jalankan `ComplianceWorkspace.exe`.

---

## Ekspor Laporan (CSV Kompatibel Excel)

Aplikasi menyediakan fitur ekspor data pada menu **Pengaturan & Backup**:
- **Ekspor Seluruh Pekerjaan (CSV)**: Menghasilkan tabel lengkap seluruh data pekerjaan compliance.
- **Ekspor Riwayat Aktivitas (CSV)**: Menghasilkan laporan audit trail log aktivitas kronologis.

Seluruh berkas CSV telah dilengkapi header UTF-8 BOM dan sanitasi karakter formula injection (`'`, `=`, `+`, `-`, `@`, `\t`, `\r`) demi keamanan analisis data pada spreadsheet.

---

## Keamanan & Privasi

- **Localhost Loopback Only**: Server hanya mengikat koneksi pada `127.0.0.1`. Koneksi dari perangkat lain pada jaringan lokal (LAN) maupun internet otomatis ditolak.
- **Validasi Host & Origin**: Mencegah serangan DNS Rebinding dan manipulasi header request.
- **Proteksi CSRF**: Memvalidasi token kriptografis acak `cw_csrf_token` pada setiap permintaan perubahan data.
- **Content Security Policy (CSP)**: Mengunci eksekusi skrip hanya dari sumber internal lokal.
- **Tanpa Telemetri**: Aplikasi sama sekali tidak mengumpulkan data penggunaan, analitik, atau berkomunikasi ke server pihak ketiga.

---

## Dokumentasi Lanjutan

Untuk informasi teknis mendalam mengenai arsitektur internal dan basis data, silakan pelajari berkas dokumentasi berikut:
- **[docs/ARCHITECTURE.md](file:///c:/Users/AXIOO%20HYPE%203%20G12/Documents/ComplianceWorkspace/docs/ARCHITECTURE.md)**: Diagram arsitektur, pipeline middleware, spesifikasi endpoint REST API, dan alur komunikasi komponen.
- **[docs/DATABASE.md](file:///c:/Users/AXIOO%20HYPE%203%20G12/Documents/ComplianceWorkspace/docs/DATABASE.md)**: Skema DDL tabel SQLite, indeks performa, transaksi atomik, trigger audit append-only, dan protokol pemulihan darurat.
- **[CHANGELOG.md](file:///c:/Users/AXIOO%20HYPE%203%20G12/Documents/ComplianceWorkspace/CHANGELOG.md)**: Catatan riwayat rilis dan fitur baseline v1.0.0.
