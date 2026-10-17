# Changelog

Semua perubahan penting pada proyek **Compliance Workspace** akan didokumentasikan dalam berkas ini.

Format berkas ini mengacu pada [Keep a Changelog](https://keepachangelog.com/id/1.0.0/), dan proyek ini mematuhi prinsip [Semantic Versioning](https://semver.org/lang/id/).

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
