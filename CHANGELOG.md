# Changelog

Semua perubahan penting pada proyek **Compliance Workspace** akan didokumentasikan dalam berkas ini.

Format berkas ini mengacu pada [Keep a Changelog](https://keepachangelog.com/id/1.0.0/), dan proyek ini mematuhi prinsip [Semantic Versioning](https://semver.org/lang/id/).

---

## [1.1.0] - 2026-10-17

Rilis besar pertama Compliance Workspace v1.1.0 menghadirkan dua pilar modul baru: **Data Import Center** dan **Work & Task Management Enhancement**, integrasi Windows Branding (PE embedded icon resource, browser favicon, dan logo horizontal), sistem migrasi skema database bertingkat (*integrity guarded*), serta pemulihan runtime tanpa restart aplikasi.

### Ditambahkan (Added)

#### 1. Data Import Center
- **Pusat Impor Data Massal**: Wizard 4-tahap (Pilih Berkas -> Pemetaan Kolom -> Pratinjau & Validasi -> Komit & Ringkasan).
- **Format Berkas Ganda**: Mendukung berkas spreadsheet Microsoft Excel (`.xlsx`) via pure-Go engine `github.com/xuri/excelize/v2` dan CSV (dengan/tanpa UTF-8 BOM).
- **Pemetaan Kolom Cerdas (*Auto-Suggest Mapping*)**: Kamus sinonim cerdas untuk kolom wajib dan opsional dalam Bahasa Indonesia dan Bahasa Inggris.
- **Validasi Read-Only Pra-Komit**: Pemeriksaan menyeluruh tanpa modifikasi database pada tahap pratinjau, menampilkan metrik total, siap impor, duplikasi, dan baris error.
- **Deteksi Duplikasi Bertingkat**: Pendeteksian duplikasi antar-baris dalam berkas yang sama (*in-file duplicate*) dan deteksi duplikasi terhadap database SQLite existing berbasis kombinasi `Judul + Tanggal Diterima + Pemberi Tugas`.
- **Mode Impor Fleksibel**: Pilihan mode penanganan duplikasi `Skip Duplicate` (lewati) dan `Create New` (tetap buat baru).
- **Snapshot Otomatis Pra-Impor**: Pembuatan cadangan otomatis database via SQLite `VACUUM INTO` sebelum transaksi komit impor dieksekusi.
- **Transaksi Atomik**: Jaminan rollback penuh database jika terjadi kegagalan sebelum komit tuntas.
- **Riwayat & Unduh Laporan Error**: Tabel riwayat batch impor dengan statistik lengkap dan tombol pengunduhan berkas laporan baris gagal/dilewati berformat CSV.
- **Pembatalan Kompensasi (*Compensate Rollback*)**: Mekanisme pembatalan batch impor dengan mengarsipkan pekerjaan baru tanpa menghapus jejak audit, dilengkapi deteksi konflik jika data telah diedit setelah impor.
- **Proteksi Sumber Daya & Keamanan**:
  - Batas ukuran berkas maksimal 10 MB atau 5.000 baris per batch.
  - Proteksi *Zip Bomb* XLSX: Batas ukuran terdekompresi 50 MB, rasio kompresi maksimal 50:1, dan batas 100.000 sel.
  - Proteksi *Spreadsheet Formula Injection*: Sanitasi otomatis karakter berisiko (`=`, `+`, `-`, `@`, `\t`, `\r`).
  - Template resmi CSV dan XLSX yang dapat diunduh langsung dari antarmuka aplikasi.

#### 2. Work & Task Management Enhancement
- **Subtask & Checklist per Pekerjaan**: Kemampuan merinci penugasan menjadi subtask terstruktur yang dapat dicentang satu per satu.
- **Kalkulasi Otomatis Progres Persentase**:
  - Kolom `progress_percentage` (0% - 100%) pada tabel `work_items`.
  - Pekerjaan dengan subtask: Progres dihitung proporsional berdasarkan rasio checklist subtask yang diselesaikan.
  - Pekerjaan tanpa subtask: Progres diestimasi otomatis berdasarkan status (`not_started` = 0%, `in_progress` = 50%, `completed` = 100%).
- **Master Kategori (*Category Management*)**:
  - Pengelolaan kategori master pada menu "Tugas & Template".
  - Dukungan warna visual kustom, deskripsi, dan status aktif/non-aktif (`is_active`).
  - Pencegahan penghapusan destruktif untuk kategori yang sedang digunakan oleh pekerjaan aktif.
- **Work Templates (Template Pekerjaan)**:
  - Fasilitas membuat dan menyimpan cetak biru penugasan kepatuhan berulang.
  - Template mencakup subtask bawaan, estimasi durasi hari, kategori, dan tingkat prioritas.
  - Aksi "Terapkan Template" untuk instan menghasilkan penugasan baru.
- **Recurring Tasks (Tugas Berkala Otomatis)**:
  - Penjadwalan pekerjaan rutin dengan frekuensi harian, mingguan, bulanan, kuartalan, atau tahunan.
  - Perlindungan *idempotency* berbasis identitas jadwal dan periode tanggal target untuk mencegah timbulnya pekerjaan duplikat.
  - Tiga pemicu otomatis: Evaluasi saat startup server, background ticker berkala setiap 1 jam, dan tombol manual "Periksa Jadwal".

#### 3. Branding & Windows Shell Integration
- **Windows Executable Icon Resource**: Penyematan ikon resmi Compliance Workspace (`ComplianceWorkspace_app.ico`, 16x16 hingga 256x256 px) ke dalam file biner `.exe` melalui Windows Resource Compiler `github.com/tc-hib/go-winres`.
- **Favicon Browser Resmi**: Favicon ICO multi-resolusi dan SVG yang dilayani langsung oleh server HTTP lokal pada endpoint `/favicon.ico` dan `/favicon.svg`.
- **Logo Horizontal Sidebar**: Penempatan logo horizontal putih resmi pada bagian atas bilah navigasi (sidebar) di bawah badge `v1.1.0 OFFLINE`.
- **Metadata PE Lengkap**: Properti berkas Windows mencakup File Description, Product Name, File Version 1.1.0.0, dan Original Filename.

#### 4. Integritas Basis Data & Migrasi Skema
- **Migrasi Inkremental v1.0.0 ke v1.1.0**:
  - `002_v1_1_schema.sql`: Menambahkan tabel `subtasks`, `categories`, `work_templates`, `recurring_schedules`, `import_batches`, `import_items`, dan kolom `progress_percentage`.
  - `003_v1_1_refinements.sql`: Pengecekan foreign key dan konsistensi skema.
  - `004_category_is_active.sql`: Penambahan kolom `is_active` pada tabel kategori.
- **Snapshot Pra-Migrasi Otomatis**: Pembuatan backup keselamatan via `VACUUM INTO` sebelum migrasi diterapkan.
- **Pemeriksaan Integritas Ganda**: Pelaksanaan `PRAGMA integrity_check` dan `PRAGMA foreign_key_check` sebelum dan sesudah migrasi.
- **Version Guard**: Menolak eksekusi aplikasi jika versi basis data lebih baru daripada batas maksimum versi yang didukung aplikasi.

### Diperbaiki (Fixed)
- **BUG-001**: Perbaikan tampilan halaman "Tugas & Template" yang sebelumnya kosong akibat exception selector DOM JavaScript.
- **BUG-002**: Perbaikan halaman "Impor Data" yang sebelumnya tidak merender wizard.
- **BUG-003**: Perbaikan perhitungan dan rendering progress bar pada kartu dashboard dan tabel register.
- **BUG-004**: Perbaikan penanganan status tombol komit impor pada antarmuka pengguna.
- **BUG-005**: Penyelarasan format tanggal ISO pada recurring generator.
- **BUG-006 (Kritis)**: Perbaikan error `sql: database is closed` setelah operasi restore database. Koneksi database SQLite kini ditangani secara dinamis melalui pointer mutasi thread-safe (`SetDB`) pada seluruh service (WorkItemService, TaskManagementService, ImportService) sehingga seluruh fitur langsung dapat digunakan kembali pasca-restore tanpa perlu mematikan atau me-restart aplikasi.
- **Penyelarasan Kontrak Data Impor (Batch 03)**: Sinkronisasi tag JSON dan normalisasi enum status/prioritas dalam Bahasa Indonesia (`Belum Mulai`, `Dalam Proses`, `Selesai`, `Tinggi`, `Rendah`, `Kritis`) untuk mendukung ekspor/impor dua arah yang mulus.

---

## [1.0.0] - 2026-10-17

> **Catatan Baseline Awal**:  
> Rilis versi 1.0.0 ini merupakan **baseline awal** pengembangan Compliance Workspace v1.0 sebelum proyek ini menggunakan sistem kontrol versi Git secara aktif. Seluruh fitur pada rilis ini telah diuji dan diverifikasi secara menyeluruh melalui serangkaian pengujian unit, integrasi, dan smoke test portabilitas.

### Ditambahkan (Added)

#### Basis Data & Migrasi Skema
- Integrasi driver SQLite murni tanpa CGO (`modernc.org/sqlite`) yang kompatibel penuh dengan kompilasi biner mandiri.
- Konfigurasi PRAGMA SQLite untuk performa dan keamanan portabel: `foreign_keys = ON`, `journal_mode = DELETE`, dan `busy_timeout = 5000`.
- Sistem migrasi otomatis berbasis file SQL terurut yang disematkan ke dalam biner (`embed.FS`) dan dicatat pada tabel `schema_migrations`.
- Skema awal basis data:
  - Tabel `work_items` untuk penyimpanan data master pekerjaan compliance.
  - Tabel `activity_logs` dengan kunci asing `ON DELETE RESTRICT`.
  - Indeks performa untuk status, tenggat waktu, tanggal penerimaan, dan pengarsipan.
  - Trigger SQLite `activity_logs_no_update` dan `activity_logs_no_delete` berbasis `RAISE(ABORT)` untuk memastikan sifat *append-only* log audit.

#### Domain Model & Lapisan Repository
- Definisi entitas `WorkItem` dan `ActivityLog` beserta konstanta status (`not_started`, `in_progress`, `waiting`, `completed`, `cancelled`) dan prioritas (`low`, `normal`, `high`, `critical`).
- Generator kode pekerjaan berurutan format `CMP-YYYY-NNNNNN` yang terkunci dalam transaksi database.
- Repository `WorkItemRepository` dengan dukungan penyaringan teks, kategori, status, prioritas, deadline, dan pengurutan dinamis.
- Repository `ActivityLogRepository` untuk pencatatan dan pembacaan riwayat aktivitas secara kronologis.
- Sinkronisasi koneksi database yang aman (*thread-safe*) via `SetDB` dengan `sync.RWMutex`.

#### Lapisan Layanan (Service Layer)
- `WorkItemService`: Mengelola transaksi atomik untuk pembuatan pekerjaan baru, pembaruan data, pengarsipan (*soft delete*), pemulihan data, serta penambahan catatan progres manual.
- `BackupService`: Mengimplementasikan pencadangan konsisten database SQLite menggunakan perintah `VACUUM INTO`, verifikasi integritas berkas cadangan melalui `PRAGMA integrity_check`, serta prosedur pemulihan (*restore*) aman dengan pembuatan snapshot darurat (*safety copy*) sebelum file aktif ditimpa.
- `ExportService`: Generator laporan berkas CSV berstandar UTF-8 dengan BOM (*Byte Order Mark*) untuk kompatibilitas instan dengan Microsoft Excel, dilengkapi sanitasi proteksi terhadap risiko *CSV Formula Injection*.

#### Server HTTP & Middleware Keamanan
- Server HTTP mandiri berbasis paket standar `net/http` yang mendengarkan secara ketat pada loopback `127.0.0.1` dengan alokasi port dinamis OS (`-port 0`) atau port spesifik (`-port <nomor>`).
- `HostValidatorMiddleware`: Memastikan Host header hanya bernilai `127.0.0.1` atau `localhost`.
- `SecurityHeadersMiddleware`: Menerapkan header proteksi HTTP ketat termasuk Content Security Policy (CSP), `X-Frame-Options: DENY`, dan `X-Content-Type-Options: nosniff`.
- `CSRFAndOriginMiddleware`: Menghasilkan token CSRF kriptografis acak 32-byte pada cookie `cw_csrf_token` bertipe `SameSite=Strict` dan memvalidasi header `X-CSRF-Token` serta header `Origin`/`Referer` pada setiap permintaan mutasi data (`POST`, `PUT`).
- Penutupan server tertib (*graceful shutdown*) yang menangani sinyal OS `SIGINT` dan `SIGTERM`.

#### Antarmuka Web Tersemat (Embedded Vanilla UI)
- Antarmuka web responsif dan minimalis berbahasa Indonesia yang disematkan langsung ke dalam biner Go melalui direktif `//go:embed static/*`.
- 100% offline tanpa CDN, tanpa external web font, dan tanpa pelacak analitik.
- Modul navigasi antarmuka:
  - **Dashboard**: Statistik ringkasan total pekerjaan, belum mulai, dalam proses, menunggu, selesai, dan melewati deadline (*overdue*).
  - **Work Register**: Tabel interaktif untuk membuat pekerjaan, melihat rincian, mengedit data, mengarsipkan pekerjaan, pencarian teks instan, dan filter multibaris.
  - **Activity Log**: Garis waktu (*timeline*) kronologis per pekerjaan dengan formulir penambahan progres manual.
  - **Deadline Tracker**: Pengelompokan visual pekerjaan berdasarkan tingkat urgensi (terlambat, hari ini, 7 hari ke depan, tanpa tenggat).
  - **Pengaturan & Backup**: Pengaturan zona waktu tampilan (default Asia/Jakarta), pembuatan snapshot backup SQLite, pemulihan database dari daftar backup, dan pengunduhan laporan CSV.
- Modul JavaScript `api.js` untuk komunikasi REST API dengan penanganan otomatis perolehan token CSRF dan percobaan ulang (*auto-retry on expired token*).

#### Pengujian & Jaminan Mutu (Quality Assurance)
- 25 skenario pengujian otomatis pada direktori `tests/` yang mencakup:
  - Pengujian atomisitas transaksi rollback.
  - Pengujian siklus pencadangan dan pemulihan database.
  - Pengujian urutan dan keunikan generator kode `CMP-YYYY-NNNNNN`.
  - Pengujian ekspor CSV, UTF-8 BOM, dan sanitasi injeksi formula.
  - Pengujian filter dan pencarian data.
  - Pengujian validasi sintaks JavaScript menggunakan `node --check` dan perimbangan kurung kurawal.
  - Pengujian integrasi seluruh endpoint HTTP API.
  - Pengujian ketiadaan dependensi jaringan/CDN (*offline check*).
  - Pengujian persistensi data setelah database ditutup dan dibuka kembali.
  - Pengujian pembatasan trigger *append-only* pada tabel log aktivitas.
  - Pengujian validasi input model domain.
  - Pengujian sanitasi terhadap potensi serangan XSS.

#### Automasi Build Portable & Distribusi
- Skrip PowerShell `scripts/build-portable.ps1` untuk mengompilasi biner mandiri Windows x64 (`ComplianceWorkspace.exe`), membuat struktur folder rilis yang bersih, menyusun dokumen `README.txt` dan `VERSION.txt`, mengompresi ke arsip ZIP `ComplianceWorkspace-v1.0-Windows-x64.zip`, dan menghitung checksum SHA-256.
- Skrip PowerShell `scripts/smoke-test-portable.ps1` untuk verifikasi smoke test portabilitas terisolasi secara otomatis di folder sandbox.
