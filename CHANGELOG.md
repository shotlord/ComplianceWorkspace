# Changelog

Semua perubahan penting pada proyek **Compliance Workspace** didokumentasikan dalam berkas ini.

Format berkas ini mengacu pada [Keep a Changelog](https://keepachangelog.com/id/1.0.0/), dan proyek ini mematuhi prinsip [Semantic Versioning](https://semver.org/lang/id/).

---

## [1.1.0] - Unreleased (Release Candidate)

Pembaruan Compliance Workspace v1.1.0 (Release Candidate) menghadirkan peningkatan signifikan pada kapabilitas manajemen penugasan kepatuhan, pusat impor data massal, siklus hidup pencadangan, penghapusan data aman berbasis audit, serta integrasi identitas visual (*branding*) Windows resmi.

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
