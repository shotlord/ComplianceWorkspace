# Panduan & Lembar Checklist User Acceptance Test (UAT)
## Compliance Workspace v1.1.0 — Work & Task Management & Data Import Center

Dokumen ini disusun sebagai panduan pengujian penerimaan pengguna (User Acceptance Testing / UAT) untuk **Compliance Workspace v1.1.0**. Pengujian ini memvalidasi seluruh fungsi baru pada modul **Work & Task Management Enhancement** dan **Data Import Center**, serta memastikan kepatuhan terhadap prinsip arsitektur *local-first*, *offline-first*, dan *append-only audit trail*.

---

### Informasi Lingkungan Pengujian UAT

| Parameter | Spesifikasi Lingkungan UAT |
| :--- | :--- |
| **Versi Aplikasi** | Compliance Workspace v1.1.0 (Pre-Release UAT Build) |
| **Paket Biner** | `uat/compliance-workspace.exe` (Windows amd64, CGO_ENABLED=0) |
| **Lokasi Database** | `uat/data/compliance.db` (Terisolasi, relatif terhadap executable) |
| **Data Cadangan** | `uat/backups/` |
| **Berkas Uji UAT** | `uat/test_data/` |
| **Persyaratan Jaringan**| **Offline Penuh** (Tidak memerlukan koneksi internet, tidak ada CDN eksternal) |
| **Host & Port** | `127.0.0.1:[OS-assigned port]` (Otomatis dibuka di browser bawaan) |

---

### Tata Cara Pelaksanaan UAT

1. **Persiapan Direktori UAT**: Pastikan direktori `uat/` telah terisolasi dari basis data v1.0.0 atau basis data operasional lainnya.
2. **Menjalankan Aplikasi**: Eksekusi berkas `uat/compliance-workspace.exe` melalui Command Prompt, PowerShell, atau klik ganda.
3. **Pencatatan Hasil**: Isi kolom **Status** (`PASS` / `FAIL`) dan berikan **Catatan Penguji** jika ditemukan perilaku yang tidak sesuai.
4. **Verifikasi Offline**: Putuskan koneksi internet (Wi-Fi/LAN) sebelum atau saat menjalankan pengujian untuk memastikan aplikasi 100% fungsional tanpa jaringan luar.

---

### Tabel Lembar Checklist Pengujian Manual (23 Skenario)

| ID | Fitur / Komponen | Langkah Pengujian Manual | Data Uji | Ekspektasi Hasil | Status (PASS/FAIL) | Catatan Penguji |
| :--- | :--- | :--- | :--- | :--- | :---: | :--- |
| **UAT-01** | Inisialisasi Database Bersih | Jalankan `compliance-workspace.exe` pada folder `uat/` yang kosong dari database. | Biner executable `uat/compliance-workspace.exe` | Aplikasi berhasil start, console menampilkan port lokal (127.0.0.1:XXXX), dan browser otomatis terbuka ke dashboard. | [ &nbsp; ] | |
| **UAT-02** | Proteksi Lokasi Basis Data | Periksa folder `uat/data/` setelah aplikasi berjalan. Verifikasi tidak ada file database yang dibuat di `%APPDATA%`, `C:\Users`, atau working directory luar. | Folder `uat/data/` | Berkas `compliance.db` terbuat di dalam `uat/data/`. Skema termigrasi otomatis hingga versi 3 dengan 5 kategori standar. | [ &nbsp; ] | |
| **UAT-03** | Impor File CSV Valid & UTF-8 BOM | Masuk ke menu **Pusat Impor Data** > **Unggah Berkas Baru**. Pilih berkas CSV valid. Klik **Lanjutkan ke Pemetaan Kolom**. | `uat/test_data/01_valid_compliance_tasks.csv` | Berkas terunggah lancar, BOM UTF-8 terdeteksi & terpotong bersih, total 5 baris data dikenali. | [ &nbsp; ] | |
| **UAT-04** | Impor File XLSX Valid | Pada wizard impor, unggah berkas spreadsheet Excel (.xlsx). Pilih sheet jika tersedia. | `uat/test_data/02_valid_compliance_tasks.xlsx` | Berkas Excel terbaca sempurna, nama worksheet teridentifikasi, dan header tabel diekstrak dengan tepat. | [ &nbsp; ] | |
| **UAT-05** | Deteksi Pemetaan Kolom (Column Mapping) | Amati dropdown pemetaan kolom pada Langkah 2 wizard impor. Coba ubah pemetaan secara manual untuk mencocokkan kolom baku. | Header berkas vs kolom baku (Judul, Deskripsi, Kategori, Pemberi Tugas, Tanggal Diterima, Tenggat, Prioritas, Status) | Sistem secara cerdas menyarankan mapping yang sesuai sinonim (misal: "Penugasan Oleh" -> "requested_by"). Pengguna dapat mengubah mapping secara fleksibel. | [ &nbsp; ] | |
| **UAT-06** | Dry-Run Preview Bersifat Read-Only | Klik **Lihat Preview Impor**. Amati tabel preview baris, validitas data, dan badge status. Periksa halaman **Daftar Pekerjaan** di tab browser baru sebelum commit. | Preview 5 baris berkas valid | Tabel preview menampilkan cuplikan data dengan indikator status hijau/valid. **Tidak ada pekerjaan baru yang masuk ke database** sebelum commit. | [ &nbsp; ] | |
| **UAT-07** | Deteksi Duplikasi (Skip vs Create) | Unggah berkas yang memuat baris duplikat in-file dan baris yang sudah ada di database. Uji mode "Lewati Duplikasi" lalu mode "Tetap Buat Baru". | `uat/test_data/04_duplicate_tasks.csv` | Sistem mendeteksi duplikasi secara akurat berdasarkan kombinasi Judul + Tanggal Diterima + Pemberi Tugas. Mode *Skip* menandai baris sebagai dilewati; mode *Create* tetap mengimpor dengan nomor CMP baru. | [ &nbsp; ] | |
| **UAT-08** | Penolakan Validasi Format Berkas & Data | Unggah berkas tanggal salah dan berkas status salah. Amati pesan kesalahan pada pratinjau. Coba juga unggah file > 10 MB atau berkas rusak (.txt/.exe). | `uat/test_data/05_invalid_dates_tasks.csv`, `06_invalid_status_priority_tasks.csv` | Baris dengan tanggal non-YYYY-MM-DD atau status/prioritas tidak baku ditandai merah (Invalid) beserta rincian pesan kesalahannya. File non-CSV/XLSX ditolak langsung. | [ &nbsp; ] | |
| **UAT-09** | Unduh Template CSV & XLSX Baku | Buka tab **Pusat Impor Data**, klik tombol **Unduh Template CSV** dan **Unduh Template XLSX**. Buka berkas yang diunduh. | Endpoint `/api/import/template/csv` dan `/api/import/template/xlsx` | Berkas CSV memuat BOM UTF-8 (terbuka rapi di Excel tanpa teks berantakan). Berkas XLSX memiliki header warna biru profesional dan contoh data kepatuhan. | [ &nbsp; ] | |
| **UAT-10** | Commit Batch Atomik & Penomoran CMP | Pada Langkah 4 wizard impor, klik **Konfirmasi & Simpan Impor**. Periksa Work Register. | Berkas valid (5 baris) | Transaksi berjalan atomik. Seluruh baris valid tersimpan, nomor kode registrasi terurut otomatis `CMP-2026-NNNNNN`, dan snapshot cadangan terbuat otomatis sebelum commit. | [ &nbsp; ] | |
| **UAT-11** | Riwayat Batch Impor & Detail Baris | Buka tab **Riwayat Batch Impor**. Klik tombol **Detail** pada salah satu batch. | Batch impor yang telah di-commit | Riwayat menampilkan kode batch (`IMP-YYYYMMDD-...`), waktu proses, jumlah berhasil, dilewati, dan gagal. Modal detail menampilkan status per nomor baris. | [ &nbsp; ] | |
| **UAT-12** | Unduh Laporan Kesalahan (Error Report CSV) | Pada riwayat impor yang memiliki baris gagal/dilewati, klik tombol **Unduh Laporan Error (CSV)**. | Batch dengan data gagal/duplikat | File CSV terunduh dengan BOM UTF-8, memuat nomor baris, status, alasan penolakan, dan rekaman data mentah JSON yang aman dari formula injection. | [ &nbsp; ] | |
| **UAT-13** | Tindakan Kompensasi Pembatalan Impor (Rollback) | Pada Riwayat Impor, klik **Batalkan Impor (Kompensasi)** pada batch: <br>a. Batch tanpa perubahan lanjutan.<br>b. Batch di mana salah satu pekerjaan telah diedit/diubah statusnya. | Batch aktif & modifikasi 1 pekerjaan | Pada (a), seluruh pekerjaan batch diarsipkan otomatis. Pada (b), sistem menampilkan **Peringatan Keras** bahwa ada pekerjaan diedit dan meminta konfirmasi eksplisit kedua sebelum mengarsipkan. Data tidak dihapus permanen! | [ &nbsp; ] | |
| **UAT-14** | Manajemen Subtask & Checklist | Buka salah satu pekerjaan di Register. Tambahkan 3 subtask / checklist baru. Ubah urutan, edit judul subtask, dan hapus salah satu subtask. | Judul subtask fiktif (misal: "Draft Kebijakan", "Review Legal", "Approval Direksi") | Subtask berhasil ditambahkan secara instan, perubahan nama tersimpan, dan checklist dapat dicentang satu per satu. Penghapusan subtask tidak merusak integritas data. | [ &nbsp; ] | |
| **UAT-15** | Perhitungan Otomatis Progres Pekerjaan | Centang 1 dari 2 checklist pada pekerjaan (50%). Tambahkan 2 checklist lagi (total 4), centang 3 checklist (75%). | Pekerjaan dengan multi-subtask | Nilai persentase progres (`progress_percentage`) terhitung otomatis secara matematis dan progress bar di UI terbarui *real-time*. Untuk pekerjaan tanpa subtask, progres mengikuti status baku. | [ &nbsp; ] | |
| **UAT-16** | Independensi Status Pekerjaan Utama | Centang seluruh checklist pekerjaan hingga progres 100%. Amati status pekerjaan utama di Register. | Pekerjaan dengan seluruh subtask tercentang | **Status pekerjaan TIDAK berubah otomatis menjadi Completed**. Pengguna tetap memegang kendali kepatuhan manual untuk mengubah status utama. | [ &nbsp; ] | |
| **UAT-17** | Manajemen Master Kategori | Buka modal **Kelola Kategori**. Tambahkan kategori baru dengan warna khusus (misal: "Tata Kelola AI", warna ungu). Coba tambahkan nama kategori yang sama. | Nama: "Kepatuhan ESG", Warna: `#10b981` | Kategori baru berhasil disimpan dan langsung muncul di dropdown kategori form pekerjaan. Duplikasi nama kategori yang sama ditolak oleh sistem. | [ &nbsp; ] | |
| **UAT-18** | Template Pekerjaan Kepatuhan | Buka tab/modal **Template Pekerjaan**. Buat template baru lengkap dengan 3 butir checklist standar. Terapkan template pada pembuatan pekerjaan baru. | Template: "Audit Triwulan APU-PPT" | Formulir pekerjaan baru otomatis terisi judul (dengan substitusi tanggal jika ada), kategori, prioritas, tenggat offset, dan butir checklist standar template. | [ &nbsp; ] | |
| **UAT-19** | Tugas Rutin Berulang (Recurring Tasks) | Buat Jadwal Rutin baru dengan frekuensi mingguan/bulanan yang terhubung ke template. Klik tombol **Periksa & Generate Jadwal Sekarang**. | Jadwal Rutin frekuensi mingguan | Generator mengeksekusi jadwal, pekerjaan baru terbuat di Work Register dengan penanda pemberi tugas `Jadwal Rutin #[ID]`, dan tanggal `next_run_date` maju ke periode berikutnya. | [ &nbsp; ] | |
| **UAT-20** | Idempotensi Tugas Berulang (Anti-Duplikasi) | Klik tombol **Periksa Jadwal** beberapa kali secara berturut-turut dalam tanggal yang sama. | Tombol manual "Periksa Jadwal Rutin" | Sistem tidak membuat pekerjaan ganda untuk jadwal dan periode yang sama. Idempotensi terjamin oleh constraint unik database dan logic service. | [ &nbsp; ] | |
| **UAT-21** | Audit Trail Append-Only (Activity Log) | Buka modal riwayat aktivitas pada pekerjaan yang dibuat melalui impor, ditambahkan subtask, atau dibatalkan melalui kompensasi. | Tab **Riwayat Aktivitas** | Seluruh aksi (dibuat via impor, subtask ditambah/dicentang, diarsipkan via kompensasi) tercatat urut waktu dalam UTC/WIB. Trigger database mencegah pengubahan atau penghapusan catatan log. | [ &nbsp; ] | |
| **UAT-22** | Keamanan Localhost & Proteksi CSRF | Buka Developer Tools (F12) > Network. Periksa header permintaan mutasi data (POST/PUT/DELETE) dan coba kirim request tanpa header `X-CSRF-Token` atau Origin yang salah. | Permintaan HTTP manipulasi API | Permintaan tanpa token CSRF yang valid ditolak dengan HTTP 403 Forbidden. Aplikasi hanya merespons binding ke `127.0.0.1`. | [ &nbsp; ] | |
| **UAT-23** | Pengoperasian Offline Total | Putuskan koneksi internet perangkat (matikan Wi-Fi). Segarkan halaman browser (F5) dan operasikan seluruh fitur aplikasi. | Operasi tanpa koneksi internet | Aplikasi beroperasi 100% normal tanpa galat CDN, font tetap tampil rapi melalui font sistem bawaan, dan tidak ada request jaringan eksternal yang gagal. | [ &nbsp; ] | |

---

### Verifikasi Khusus Pre-UAT (Integritas Teknis)

Sebelum menyerahkan aplikasi untuk pengujian pengguna, pengembang telah memvalidasi:
1. **Pencegahan Zip Bomb / Decompression Exhaustion**: Ekstraksi file XLSX dibatasi maksimal 50 MB terdekompresi dan rasio kompresi 50:1.
2. **Pencegahan Formula Injection**: Nilai teks sah yang diawali tanda hubung (`-`) atau nomor kontak (`+62`) disimpan secara utuh ke database tanpa modifikasi, sementara saat diekspor ke CSV, formula injection dinetralkan secara ketat.
3. **Kompensasi Rollback Terlindungi**: Pekerjaan yang telah dimodifikasi pasca-impor dilindungi dengan peringatan eksplisit dan membutuhkan konfirmasi `force=true`.
4. **Migrasi Inkremental Database**: Database v1.0.0, v2, dan database baru termigrasi mulus ke versi skema 3 dengan integritas referensial dan foreign keys tervalidasi `ok`.

---

### Tanda Tangan Persetujuan UAT

| Peran | Nama Penguji / Reviewer | Tanggal Pengujian | Rekomendasi (Diterima / Perlu Perbaikan) | Tanda Tangan |
| :--- | :--- | :--- | :--- | :--- |
| **Compliance Officer (Pengguna Utama)** | | | | |
| **Lead Technical QA** | | | | |
| **Product Owner** | | | | |

---
*Dokumen ini merupakan bagian resmi dari siklus penjaminan kualitas Compliance Workspace v1.1.0.*
