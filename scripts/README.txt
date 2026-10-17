================================================================================
                 PANDUAN PENGGUNAAN COMPLIANCE WORKSPACE v1.1.0
                  Aplikasi Portabel Work & Activity Tracker
================================================================================

1. TENTANG APLIKASI
--------------------------------------------------------------------------------
Compliance Workspace v1.1.0 adalah aplikasi web lokal, mandiri (portabel), dan
offline-first yang dirancang khusus untuk Compliance Officer guna mengelola:
1. Work Register: Pencatatan penugasan kepatuhan dengan nomor CMP-YYYY-NNNNNN.
2. Subtasks & Checklist: Rincian tugas bertahap dengan kalkulasi otomatis progres (0-100%).
3. Category Management: Klasifikasi pekerjaan berbasis kategori master warna visual.
4. Work Templates: Cetak biru pekerjaan siap pakai untuk standardisasi alur kerja.
5. Recurring Tasks: Penjadwalan pekerjaan berulang otomatis dengan proteksi idempotency.
6. Data Import Center: Wizard impor data massal dari file CSV dan Excel (XLSX).
7. Activity Log: Jejak audit kronologis yang bersifat append-only.
8. Backup & Restore: Pencadangan konsisten SQLite VACUUM dan pemulihan data instan.

Seluruh antarmuka frontend (HTML/CSS/JS) disematkan secara langsung di dalam file
biner ComplianceWorkspace.exe, dan seluruh data disimpan secara lokal dalam basis data
SQLite murni tanpa ketergantungan internet, cloud, Node.js, atau database server terpisah.


2. PERSYARATAN SISTEM
--------------------------------------------------------------------------------
- Sistem Operasi : Windows 10 atau Windows 11 (64-bit / x64).
- Hak Akses      : Hak pengguna standar (Non-Administrator dapat menjalankan).
- Peramban Web   : Microsoft Edge, Google Chrome, Mozilla Firefox, atau browser modern.
- Koneksi        : 100% Offline (tidak membutuhkan akses internet maupun API keys).


3. CARA MENJALANKAN APLIKASI
--------------------------------------------------------------------------------
Cara 1: Klik Ganda (Double Click)
  - Klik ganda pada file "ComplianceWorkspace.exe".
  - Sebuah jendela konsol kecil akan terbuka dan menampilkan informasi server:
      [INFO] Server aktif dan mendengarkan pada: http://127.0.0.1:54321
  - Buka browser web favorit Anda dan akses alamat URL yang tertera tersebut.

Cara 2: Melalui Terminal (Command Prompt / PowerShell)
  - Buka PowerShell atau Command Prompt pada folder aplikasi ini.
  - Jalankan perintah:
      .\ComplianceWorkspace.exe
    atau jika ingin menentukan nomor port tertentu:
      .\ComplianceWorkspace.exe -port 8080
  - Akses http://127.0.0.1:8080 melalui peramban web Anda.


4. CARA MENGHENTIKAN APLIKASI
--------------------------------------------------------------------------------
- Pada jendela konsol/terminal, tekan kombinasi tombol [Ctrl + C], atau tutup
  jendela konsol tersebut secara langsung.
- Aplikasi akan melakukan penutupan secara tertib (graceful shutdown) guna
  memastikan seluruh transaksi basis data SQLite tersimpan sempurna tanpa kerusakan.


5. STRUKTUR FOLDER APLIKASI
--------------------------------------------------------------------------------
Folder rilis portabel memiliki struktur berikut:

ComplianceWorkspace-v1.1.0-Windows-x64/
│
├── ComplianceWorkspace.exe  -> File program utama aplikasi (dengan ikon resmi tersemat)
├── README.txt               -> Petunjuk penggunaan dan operasional ini
├── VERSION.txt              -> Rincian rilis, versi skema, dan konfigurasi build
│
├── data/                    -> Folder penyimpanan basis data utama SQLite
│   └── compliance.db        -> Dibuat otomatis saat aplikasi pertama kali dijalankan
│
├── backups/                 -> Folder tempat berkas cadangan database disimpan
│   └── compliance_backup_*.db -> File cadangan hasil pencadangan berkala/otomatis
│
└── exports/                 -> Folder tempat penyimpanan berkas laporan CSV hasil unduhan


6. MODUL BARU PADA v1.1.0
--------------------------------------------------------------------------------
A. Subtask, Checklist & Progress Percentage:
   - Setiap pekerjaan kini dapat dipecah menjadi subtask/checklist terperinci.
   - Progres dihitung otomatis: Jika pekerjaan memiliki subtask, progres mencerminkan
     persentase checklist yang telah dicentang. Jika tidak memiliki subtask, progres
     mengikuti status (Belum Mulai = 0%, Dalam Proses = 50%, Selesai = 100%).

B. Kategori Master:
   - Pengelolaan kategori pekerjaan pada menu "Tugas & Template".
   - Kategori dapat diatur warna visualnya, dinonaktifkan tanpa menghapus data historis,
     serta dicegah penghapusan jika masih digunakan oleh pekerjaan aktif.

C. Work Templates (Template Pekerjaan):
   - Standarisasi penugasan berulang dengan membuat template pekerjaan.
   - Template dapat memuat daftar subtask bawaan, kategori, prioritas, dan estimasi hari.
   - Klik "Terapkan Template" untuk langsung menghasilkan pekerjaan baru.

D. Recurring Tasks (Tugas Berkala Otomatis):
   - Menjadwalkan pekerjaan yang berulang secara harian, mingguan, bulanan, kuartalan,
     atau tahunan.
   - Menerapkan perlindungan idempotency berbasis ID jadwal dan periode target untuk
     mencegah duplikasi ganda.
   - Pemicu bekerja otomatis pada startup server, background ticker setiap 1 jam,
     dan tombol manual "Periksa Jadwal".

E. Data Import Center (Pusat Impor Data Massal):
   - Mendukung berkas Microsoft Excel (.xlsx) dan CSV (dengan/tanpa UTF-8 BOM).
   - Wizard 4 tahap: Pilih Berkas -> Pemetaan Kolom -> Pratinjau & Validasi -> Komit.
   - Validasi read-only pada tahap pratinjau tanpa mengubah database.
   - Deteksi duplikasi cerdas: duplikasi antar-baris dalam file dan duplikasi terhadap
     database SQLite existing.
   - Mode impor fleksibel: Lewati duplikasi (Skip) atau Tetap buat baru (Create).
   - Pembuatan cadangan otomatis (pre-commit safety backup) sebelum impor disimpan.
   - Riwayat batch impor dan fitur kompensasi pembatalan (Compensate Rollback).


7. PROSEDUR UPGRADE DARI VERSI v1.0.0 KE v1.1.0
--------------------------------------------------------------------------------
Jika Anda telah menggunakan Compliance Workspace v1.0.0 sebelumnya:

LANGKAH 1 — PENCADANGAN PENGAMAN:
1. Pastikan aplikasi v1.0.0 telah dihentikan (tidak sedang berjalan).
2. Salin folder "data/" Anda yang berisi "compliance.db" ke tempat penyimpanan cadangan.

LANGKAH 2 — PENEMPATAN BINER BARU:
1. Salin file "ComplianceWorkspace.exe" versi v1.1.0 ke dalam folder aplikasi Anda,
   menggantikan file "ComplianceWorkspace.exe" versi lama.
2. Pastikan file "VERSION.txt" dan "README.txt" diperbarui ke versi v1.1.0.
3. JANGAN MENGHAPUS folder "data/".

LANGKAH 3 — MIGRASI OTOMATIS:
1. Jalankan "ComplianceWorkspace.exe" v1.1.0.
2. Sistem migrasi database incremental akan langsung mendeteksi skema v1.0.0 Anda,
   membuat salinan cadangan pra-migrasi otomatis, menjalankan PRAGMA integrity check,
   dan menerapkan pembaruan tabel v1.1.0 tanpa merusak data pekerjaan lama Anda.
3. Seluruh pekerjaan v1.0.0 Anda tetap utuh dan langsung memperoleh fitur subtask,
   kategori master, dan progres persentase.


8. PENCADANGAN (BACKUP) & PEMULIHAN (RESTORE) DATA
--------------------------------------------------------------------------------
Pencadangan (Backup):
1. Buka aplikasi di peramban, lalu klik menu "Pengaturan & Backup".
2. Klik tombol "Buat Cadangan Baru".
3. Sistem membuat snapshot konsisten SQLite ke dalam folder "backups/".
4. Integritas diperiksa otomatis dengan PRAGMA integrity_check dan foreign_key_check.

Pemulihan (Restore):
1. Pada menu "Pengaturan & Backup", pilih salah satu file cadangan dari tabel riwayat.
2. Klik tombol "Pulihkan".
3. Sistem membuat snapshot pengaman darurat dari database aktif sebelum penimpaan.
4. Database dipulihkan seketika dan SELURUH service langsung dapat digunakan kembali
   tanpa perlu mematikan atau me-restart aplikasi.


9. PRAKTIK TERBAIK & CATATAN KEAMANAN
--------------------------------------------------------------------------------
A. Peringatan Keamanan Windows SmartScreen:
   Karena aplikasi ini didistribusikan secara portabel tanpa sertifikat digital berbayar,
   Windows Defender SmartScreen mungkin menampilkan peringatan "Windows protected your PC".
   -> Klik "More info" (Informasi selengkapnya), lalu klik "Run anyway" (Tetap jalankan).

B. Penyimpanan Portabel:
   Disarankan menyimpan folder aplikasi di penyimpanan internal (SSD/HDD) seperti
   "C:\Users\<Nama>\Documents\ComplianceWorkspace". Hindari menjalankan database SQLite
   aktif langsung dari flashdisk USB yang rentan terlepas saat operasi penulisan.

C. Hindari Folder Cloud Sync Aktif:
   Jangan meletakkan folder data aplikasi di dalam folder yang sedang disinkronkan aktif
   oleh OneDrive, Dropbox, atau Google Drive karena penguncian file sinkronisasi dapat
   memicu konflik SQLite lock.

D. Batasan Akses Jaringan:
   Aplikasi HANYA mengikat ke 127.0.0.1 (loopback lokal). Perangkat lain di jaringan
   tidak dapat mengakses antarmuka ini. Tidak ada data yang dikirim ke internet.
================================================================================
