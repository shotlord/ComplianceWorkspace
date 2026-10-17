================================================================================
                 PANDUAN PENGGUNAAN COMPLIANCE WORKSPACE v1.0
                  Aplikasi Portabel Work & Activity Tracker
================================================================================

1. TENTANG APLIKASI
--------------------------------------------------------------------------------
Compliance Workspace v1.0 adalah aplikasi web lokal dan mandiri (portabel) yang
dirancang khusus untuk Compliance Officer guna mencatat penugasan (Work Items)
dan riwayat aktivitas (Activity Log) tanpa ketergantungan pada koneksi internet,
layanan cloud, atau instalasi runtime tambahan (tanpa Python, Node.js, maupun
server database terpisah).

Seluruh antarmuka frontend (HTML/CSS/JS) telah disematkan secara langsung di
dalam file biner ComplianceWorkspace.exe, dan seluruh data disimpan secara
lokal dalam basis data SQLite murni.


2. PERSYARATAN SISTEM
--------------------------------------------------------------------------------
- Sistem Operasi : Windows 10 atau Windows 11 (64-bit / x64).
- Hak Akses      : Hak pengguna standar (Non-Administrator dapat menjalankan).
- Peramban Web   : Microsoft Edge, Google Chrome, Mozilla Firefox, atau browser
                   modern lainnya.
- Koneksi        : 100% Offline (tidak membutuhkan akses internet).


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
  memastikan seluruh transaksi basis data dan berkas tersimpan sempurna
  tanpa ada data yang terputus atau rusak.


5. STRUKTUR FOLDER APLIKASI
--------------------------------------------------------------------------------
Folder rilis portabel memiliki struktur berikut:

ComplianceWorkspace-v1.0-Windows-x64/
│
├── ComplianceWorkspace.exe  -> File program utama aplikasi
├── README.txt               -> Petunjuk penggunaan dan operasional ini
├── VERSION.txt              -> Rincian rilis, versi, dan konfigurasi build
│
├── data/                    -> Folder penyimpanan basis data utama SQLite
│   └── compliance.db        -> Dibuat otomatis saat aplikasi pertama kali dijalankan
│
├── backups/                 -> Folder tempat berkas cadangan database disimpan
│   └── backup_*.db          -> File cadangan hasil ekspor/backup konsisten
│
└── exports/                 -> Folder tempat penyimpanan berkas laporan CSV
                             yang Anda unduh melalui aplikasi


6. CARA MEMINDAHKAN APLIKASI KE LAPTOP LAIN (PORTABILITAS)
--------------------------------------------------------------------------------
Aplikasi ini bersifat 100% portabel:
1. Pastikan aplikasi telah dimatikan (tidak sedang berjalan).
2. Salin SELURUH folder "ComplianceWorkspace-v1.0-Windows-x64" (termasuk folder
   data/ yang berisi file compliance.db Anda) ke Flashdisk, hard drive eksternal,
   atau bagikan melalui jaringan lokal.
3. Letakkan folder tersebut di laptop tujuan (misalnya di folder Dokumen atau
   Desktop laptop baru).
4. Klik ganda "ComplianceWorkspace.exe" pada laptop baru tersebut.
5. Seluruh data pekerjaan dan riwayat aktivitas Anda akan langsung tersedia
   persis seperti sebelumnya tanpa perlu konfigurasi apa pun.


7. PENCADANGAN (BACKUP) & PEMULIHAN (RESTORE) DATA
--------------------------------------------------------------------------------
Aplikasi telah dilengkapi fitur pencadangan konsisten berbasis SQLite VACUUM:

Pencadangan (Backup):
1. Buka aplikasi di peramban, lalu klik menu "Pengaturan & Backup" di bilah sisi.
2. Klik tombol "Buat Cadangan Baru".
3. Sistem akan membuat snapshot database yang konsisten ke dalam folder "backups/".
4. Integritas file cadangan diperiksa secara otomatis menggunakan PRAGMA integrity_check.

Pemulihan (Restore):
1. Pada menu "Pengaturan & Backup", pilih salah satu cadangan dari daftar.
2. Klik tombol "Pulihkan".
3. Konfirmasikan tindakan. Sebelum melakukan restore, sistem secara otomatis
   membuat salinan cadangan pengaman dari database yang sedang aktif saat ini.
4. Data akan dipulihkan dan antarmuka akan dimuat ulang.

Pencadangan Manual:
- Anda juga dapat menyalin berkas "data/compliance.db" secara manual ke lokasi
  penyimpanan aman lain KETIKA APLIKASI SEDANG TIDAK BERJALAN.


8. EKSPOR DATA KE FORMAT EXCEL (CSV)
--------------------------------------------------------------------------------
1. Buka menu "Pengaturan & Backup".
2. Pada bagian "Ekspor Data (CSV)", Anda dapat memilih:
   - "Ekspor Seluruh Pekerjaan (CSV)": mengunduh seluruh daftar pekerjaan.
   - "Ekspor Riwayat Aktivitas (CSV)": mengunduh log audit aktivitas.
3. Berkas CSV diformat dengan UTF-8 with BOM agar langsung terbaca rapi oleh
   Microsoft Excel tanpa teks berantakan.
4. Seluruh teks telah diproteksi dari risiko Formula Injection demi keamanan data.


9. CARA MEMPERBARUI APLIKASI DI MASA DEPAN (UPDATE TANPA HILANG DATA)
--------------------------------------------------------------------------------
Jika di masa mendatang tersedia pembaruan versi (misalnya v1.1):
1. Matikan aplikasi Compliance Workspace v1.0 yang sedang berjalan.
2. Salin file "ComplianceWorkspace.exe" yang baru dan timpa (overwrite) file
   "ComplianceWorkspace.exe" lama di folder aplikasi Anda.
3. JANGAN MENGHAPUS ATAU MENGUBAH folder "data/".
4. Jalankan executable yang baru. Migrasi database internal akan dijalankan
   secara otomatis dan menyesuaikan skema tanpa menghilangkan data Anda.


10. PRAKTIK TERBAIK & CATATAN KEAMANAN PENTING
--------------------------------------------------------------------------------
A. Peringatan Keamanan Windows SmartScreen:
   Karena aplikasi ini didistribusikan secara portabel tanpa sertifikat digital
   berbayar (Code Signing Certificate), Windows Defender SmartScreen mungkin
   menampilkan jendela peringatan biru "Windows protected your PC".
   -> Ini adalah perilaku standar Windows untuk program baru yang belum dikenal.
   -> Untuk melanjutkan: Klik teks "More info" (Informasi selengkapnya), lalu
      klik tombol "Run anyway" (Tetap jalankan).

B. Jangan Menjalankan Langsung dari Flashdisk / USB Drive yang Lambat:
   Disarankan untuk selalu menyalin folder aplikasi ke penyimpanan internal
   laptop (SSD/HDD) seperti folder "C:\Users\<Nama>\Documents". Menjalankan
   database SQLite langsung di atas flashdisk USB berisiko menyebabkan kerusakan
   data (file corruption) jika flashdisk tiba-tiba terlepas saat proses penulisan.

C. Hindari Folder Sinkronisasi Cloud Aktif (OneDrive, Dropbox, Google Drive):
   Jangan meletakkan folder aktif aplikasi di dalam direktori yang sedang
   disinkronkan secara langsung oleh layanan cloud sync. Layanan cloud sync
   sering kali mengunci berkas sementara saat mengunggah, yang dapat memicu
   gangguan kunci berkas (file lock conflict) pada database SQLite.

D. Batasan Akses Jaringan:
   Aplikasi hanya mendengarkan pada alamat internal loopback 127.0.0.1. Perangkat
   atau komputer lain di jaringan Anda TIDAK DAPAT mengakses aplikasi ini.
   Aplikasi tidak melakukan panggilan keluar (outbound telemetry) ke internet.

E. Integritas Data & Append-Only Audit Log:
   Seluruh riwayat penugasan dan aktivitas compliance dirancang append-only.
   Penghapusan destruktif ditiadakan; pekerjaan yang tidak lagi aktif dapat
   diarsipkan melalui tombol "Arsipkan".
================================================================================
