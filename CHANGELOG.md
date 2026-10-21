# Changelog

Semua perubahan penting pada proyek **Compliance Workspace** didokumentasikan dalam berkas ini.

Format berkas ini mengacu pada [Keep a Changelog](https://keepachangelog.com/id/1.0.0/), dan proyek ini mematuhi prinsip [Semantic Versioning](https://semver.org/lang/id/).

---

## [1.1.1] - Unreleased

Patch ini menyelesaikan Security Hardening Batch A–D serta perbaikan reliabilitas, pemulihan database, dan konsistensi antarmuka yang ditemukan selama QA dan UAT. Database schema tetap pada versi 6; patch ini tidak menambahkan atau mengubah skema database.

### Diubah (Changed)
- **UAT-111-02**: Memperjelas keterangan dan format kolom pada wizard impor, termasuk format tanggal `YYYY-MM-DD`, nilai enum prioritas, serta penandaan kolom wajib dan opsional.
- **UAT-111-03**: Menyeragamkan kapitalisasi judul, label, tombol, petunjuk, notifikasi, validasi, dan empty state sesuai fungsi masing-masing elemen antarmuka.
- **UAT-111-05**: Menyelaraskan penulisan antarmuka dan pesan backend yang ditampilkan kepada pengguna agar lebih jelas, ringkas, dan konsisten dalam bahasa Indonesia, termasuk istilah pekerjaan, jadwal berulang, cadangan, serta referensi nama tombol.

### Keamanan (Security)
- Validasi `Host`, `Origin`, dan `Referer` menggunakan authority loopback dan port listener yang tepat untuk mencegah prefix-bypass dan DNS rebinding.
- Cookie CSRF menjadi `HttpOnly` + `SameSite=Strict`; token divalidasi sebagai nilai hex 32-byte dan dibandingkan secara constant-time.
- CSP tidak lagi mengizinkan `script-src 'unsafe-inline'`; ditambahkan `object-src 'none'`, `base-uri 'none'`, `frame-ancestors 'none'`, dan `form-action 'self'`.
- Menghapus penggunaan `innerHTML` untuk pesan error dan metadata dinamis yang dapat berasal dari respons server.
- Menambahkan batas payload JSON dan multipart, validasi content type, penolakan JSON ambigu atau unknown field, serta pembersihan file multipart sementara.
- Memperketat validasi path backup dan recovery untuk menolak traversal, symbolic link, junction atau reparse point, direktori yang menyamar sebagai artefak, serta file non-reguler.
- Menambahkan read-header, read, write, idle timeout, dan batas header eksplisit pada server HTTP lokal.

### Diperbaiki (Fixed)
- Safety snapshot restore sekarang wajib berhasil dan lolos `integrity_check` serta `foreign_key_check`; error `VACUUM INTO` tidak lagi diabaikan.
- Restore menggunakan salinan staging pada filesystem yang sama, migrasi dan verifikasi sebelum aktivasi, pertukaran file via rename, serta rollback file dan koneksi pada seluruh failure path kritis.
- Menambahkan rekonsiliasi startup (`database.RecoverInterruptedRestore`) sebelum inisialisasi SQLite untuk memulihkan database asli dari rollback jika restore terinterupsi, serta menolak database aktif kosong, korup, atau tidak memiliki struktur aplikasi yang sah sebelum migrasi dapat membuat database baru.
- **QA-RC-01**: Menyatukan validasi database pada startup recovery, inspeksi cadangan, dan restore melalui `database.ValidateDatabaseFile`, termasuk `integrity_check`, `foreign_key_check`, riwayat migrasi tanpa gap, serta tabel dan kolom wajib sesuai schema v1–v6. Staging divalidasi sebelum aktivasi dan kembali diverifikasi setelah migrasi; database schema lama yang didukung tetap dapat dimigrasikan.
- Inspeksi backup menggunakan koneksi SQLite read-only agar berkas sumber tidak termodifikasi.
- **QA-RC-02**: Menghilangkan pengambilan read-lock operation gate secara rekursif pada permintaan pemeriksaan jadwal berulang, sambil mempertahankan koordinasi request biasa, background recurring task, dan exclusive restore.
- **UAT-111-01**: Memperbaiki default pemetaan kolom judul impor CSV agar memilih `Judul`, tidak keliru memakai `Kode Pekerjaan`, meminta pemetaan manual jika header judul ambigu, dan tetap mendeteksi serta melewati data yang sudah ada pada mode skip.
- **UAT-111-04**: Menyamakan rentang Deadline Tracker dengan Dashboard sehingga kategori Tujuh Hari Mendatang hanya memuat pekerjaan bertenggat setelah hari ini sampai tujuh hari kalender ke depan, termasuk batas hari ketujuh dan tidak termasuk tanggal setelahnya.
- **REL-111-01**: Packaging rilis kini mewajibkan executable sumber yang dipilih secara eksplisit, menolak sumber selain PE v1.1.1 yang sesuai, mencegah overwrite paket yang sudah ada, dan mencatat checksum SHA-256 executable serta paket ZIP pada berkas checksum khusus v1.1.1.

### Pengujian (Tests)
- Menambahkan fault-injection untuk snapshot korup, kegagalan safety snapshot, kegagalan sesudah aktivasi staging, dan preservasi database aktif pada restore yang ditolak.
- Menambahkan rangkaian pengujian crash recovery untuk restore terinterupsi, database aktif kosong atau korup, rollback korup atau ambigu, instalasi awal, kompatibilitas schema lama, riwayat migrasi yang tidak kontinu, serta artefak symlink, junction, dan reparse point Windows.
- Menambahkan regression test struktur backup/restore, migrasi schema v1 yang didukung, dan interleaving deterministik antara recurring operation dengan restore untuk mencegah deadlock.
- Menambahkan regresi exact Host/Origin, atribut CSRF, CSP, malformed atau oversized JSON, oversized multipart, dan symlink backup.
- Menambahkan regresi UAT untuk default mapping dan deteksi duplikasi impor, string UX penting, serta batas dan eksklusivitas kategori Deadline Tracker.
- Menambahkan regresi packaging terisolasi untuk sumber yang hilang, penolakan executable v1.1.0, penerimaan executable v1.1.1, preservasi output existing, dan pencatatan checksum.

---

## [1.1.0] - 2026-10-18

Pembaruan Compliance Workspace v1.1.0 menghadirkan peningkatan signifikan pada kapabilitas manajemen penugasan kepatuhan, pusat impor data massal, siklus hidup pencadangan, penghapusan data aman berbasis audit, serta integrasi identitas visual (*branding*) Windows resmi.

### Ditambahkan (Added)

#### 1. Data Import Center
- **Pusat Impor Data Massal**: Wizard 4-tahap (Pilih Berkas -> Pemetaan Kolom -> Pratinjau & Validasi -> Komit & Ringkasan).
- **Dukungan Format Ganda**: Pemrosesan berkas Microsoft Excel (`.xlsx`) via pure-Go engine `github.com/xuri/excelize/v2` dan CSV (dengan/tanpa UTF-8 BOM).
- **Pemetaan Kolom Cerdas (*Auto-Suggest Mapping*)**: Kamus sinonim cerdas untuk pemetaan kolom baku secara otomatis dalam Bahasa Indonesia dan Bahasa Inggris.
- **Validasi Read-Only Pra-Komit**: Pemeriksaan menyeluruh tanpa modifikasi database pada tahap pratinjau, menampilkan metrik total, baris siap impor, duplikasi, dan baris galat.
- **Deteksi Duplikasi Bertingkat**: Pendeteksian duplikasi antar-baris dalam berkas yang sama (*in-file duplicate*) dan deteksi duplikasi terhadap database SQLite existing berbasis kombinasi `Judul + Tanggal Diterima + Pemberi Tugas`.
- **Mode Impor Fleksibel**: Pilihan penanganan duplikasi `Skip Duplicate` (lewati) dan `Create New` (tetap buat baru).
- **Snapshot Otomatis Pra-Impor**: Pembuatan cadangan otomatis database via SQLite `VACUUM INTO` sebelum transaksi komit impor dieksekusi.
- **Transaksi Atomik**: Jaminan rollback penuh database jika terjadi kendala sebelum komit tuntas.
- **Riwayat & Laporan Galat**: Tabel riwayat batch impor dengan statistik lengkap serta unduhan berkas laporan baris gagal/dilewati berformat CSV.
- **Pembatalan Kompensasi (*Compensate Rollback*)**: Mekanisme pembatalan batch impor dengan mengarsipkan pekerjaan terkait tanpa merusak jejak audit.
- **Template Resmi**: Template CSV dan XLSX yang dapat diunduh langsung dari antarmuka aplikasi.

#### 2. Work & Task Management Enhancement
- **Subtask & Checklist per Pekerjaan**: Kemampuan merinci penugasan menjadi butir checklist terstruktur yang dapat dicentang bertahap.
- **Kalkulasi Otomatis Progres Persentase**: Kolom `progress_percentage` (0% - 100%) dihitung otomatis berbasis rasio subtask selesai atau estimasi status pekerjaan.
- **Master Kategori (*Category Management*)**: Pengelolaan klasifikasi pekerjaan dengan warna visual kustom, deskripsi, dan status aktif/non-aktif (`is_active`).
- **Work Templates (Template Pekerjaan)**: Cetak biru penugasan kepatuhan berulang dengan subtask bawaan, estimasi durasi hari, kategori, dan prioritas.
- **Recurring Tasks (Tugas Berkala Otomatis)**: Penjadwalan pekerjaan rutin (harian, mingguan, bulanan, kuartalan, tahunan) dengan proteksi *idempotency* anti-duplikasi.
- **Pelacakan Provenance**: Kolom `provenance` pada tabel `work_items` untuk melacak asal muasal pembentukan pekerjaan (`manual`, `template`, `recurring`, `import`).

#### 3. Siklus Hidup Pencadangan & Penghapusan Aman
- **Manajemen Cadangan Berkala**: Aksi hapus berkas cadangan per file dan penghapusan massal (*batch delete*) dengan jaminan retensi minimal 1 berkas cadangan valid di disk.
- **Penghapusan Aman Berbasis Audit (*Tombstone Pattern*)**: Kolom `deleted_at` pada `work_items` untuk menghapus penugasan dari operasional aktif dan membersihkan subtask, namun tetap mempertahankan jejak audit permanen pada `activity_logs`.
- **Pemulihan Runtime Hot-Restore**: Pembaruan dinamis *database connection pool* (`SetDB`) pada seluruh service sehingga aplikasi langsung dapat digunakan kembali pasca-restore tanpa restart.

#### 4. Branding & Integrasi Windows
- **Ikon Executable Windows**: Penyematan ikon resmi Compliance Workspace (`ComplianceWorkspace_app.ico`, 16x16 hingga 256x256 px) ke dalam biner `.exe` melalui `go-winres`.
- **Favicon Browser & Logo Horizontal**: Favicon multi-resolusi ICO/SVG dan logo horizontal resmi pada bilah navigasi (sidebar).
- **Metadata PE Lengkap**: Informasi berkas Windows mencakup File Description, Product Name, File Version 1.1.0.0, dan Original Filename.

### Diubah (Changed)
- **Simplifikasi Modal Interaktif**: Menyederhanakan modal *Hapus Permanen* dan *Pemulihan Basis Data* dengan konfirmasi persetujuan risiko eksplisit tanpa ketergantungan *pre-fetch* jaringan yang dapat menahan pembukaan antarmuka.
- **Penyelarasan Nilai Frekuensi**: Standarisasi nilai frekuensi recurring schedule pada antarmuka pengguna agar sinkron dengan validasi domain backend (`daily`, `weekly`, `monthly`, `quarterly`, `yearly`).

### Diperbaiki (Fixed)
- **BUG-006**: Perbaikan error `sql: database is closed` setelah operasi restore database melalui mutasi koneksi thread-safe.
- **BUG-CAT-001**: Pemisahan tegas antara deaktivasi kategori (`is_active = 0`) dan penghapusan permanen kategori.
- **BUG-TPL-002**: Penanganan loading state tabel template pekerjaan dan deserialisasi JSON checklist items.
- **BUG-REC-003**: Sinkronisasi jadwal berulang dan integrasi registrasi pekerjaan dengan status aktif yang konsisten.
- **BUG-IMP-001**: Sinkronisasi riwayat impor data dengan transaksi SQLite agar batch langsung tercatat pada audit trail.

### Keamanan (Security)
- **Proteksi Zip Bomb XLSX**: Pembatasan ukuran terdekompresi maksimal 50 MB, rasio kompresi 50:1, dan batas 100.000 sel.
- **Sanitasi CSV Formula Injection**: Sanitasi otomatis karakter berisiko (`=`, `+`, `-`, `@`, `\t`, `\r`) pada impor dan ekspor CSV.
- **Verifikasi Integritas SQLite**: Pemeriksaan `PRAGMA integrity_check` dan validasi versi skema sebelum mengeksekusi pemulihan basis data.

---

## [1.0.0] - 2026-10-17

Rilis baseline awal Compliance Workspace v1.0.

### Ditambahkan (Added)
- Integrasi driver SQLite murni tanpa CGO (`modernc.org/sqlite`).
- Konfigurasi PRAGMA SQLite portabel: `foreign_keys = ON`, `journal_mode = DELETE`, dan `busy_timeout = 5000`.
- Skema dasar `work_items`, `activity_logs`, dan trigger append-only `activity_logs_no_update` / `activity_logs_no_delete`.
- Generator kode pekerjaan terurut `CMP-YYYY-NNNNNN`.
- Server HTTP lokal loopback `127.0.0.1` dengan validasi Host, proteksi CSRF, dan Content Security Policy.
- Antarmuka web modern disematkan langsung melalui Go `embed`.
- Fitur ekspor CSV dengan header UTF-8 BOM dan sanitasi formula.
- Pencadangan konsisten via SQLite `VACUUM INTO`.
