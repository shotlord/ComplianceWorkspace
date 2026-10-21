# Kebijakan Keamanan

## Versi yang Didukung

Selama v1.1.1 masih berstatus `Unreleased`, v1.1.0 adalah rilis stabil terakhir. Source, hasil UAT, atau executable kandidat tidak menjadi rilis stabil sampai Project Owner mengotorisasi packaging dan publikasi.

## Melaporkan Kerentanan

Jangan memasukkan data perusahaan, database pengguna, dokumen rahasia, kredensial, atau bukti sensitif ke issue publik maupun repositori. Laporkan secara privat kepada pemilik repositori dengan informasi minimum berikut:

- versi aplikasi dan sistem operasi;
- langkah reproduksi menggunakan data fiktif;
- dampak yang diamati;
- log atau tangkapan layar yang sudah disanitasi;
- apakah database aktif, backup, atau audit trail berpotensi terdampak.

Jangan mengunggah `data/compliance.db`, isi `backups/`, UAT database, atau paket internal untuk membuktikan temuan.

## Model Keamanan

Compliance Workspace adalah aplikasi personal, single-user, local-first, dan offline. Server hanya boleh mengikat ke `127.0.0.1` pada port yang dialokasikan OS. Aplikasi tidak menyediakan autentikasi jaringan dan tidak dirancang untuk dipublikasikan ke LAN atau internet.

Kontrol utama meliputi:

- validasi exact Host/Origin/Referer pada authority loopback dan proteksi CSRF untuk request yang mengubah state;
- CSP, header keamanan HTTP, request-size limits, dan rendering/escaping data tidak tepercaya pada jalur UI yang diaudit;
- parameter binding SQL dan validasi input;
- activity log append-only;
- backup konsisten SQLite melalui `VACUUM INTO` dan validasi database read-only;
- validasi struktur schema v1–v6, migration history kontinu, `integrity_check`, dan `foreign_key_check` pada startup recovery, inspeksi backup, dan restore;
- restore berbasis staging, safety snapshot, operation gate, aktivasi rename, rollback, dan recovery startup fail-closed;
- penolakan file/direktori recovery yang tidak aman, termasuk symlink serta Windows junction/reparse point pada jalur yang divalidasi;
- tidak ada CDN, telemetri, cloud sync, auto-update, atau runtime API key.

Kontrol tersebut tidak menyediakan autentikasi multi-user, enkripsi database bawaan, code signing, atau perlindungan terhadap pengguna lokal yang sudah memiliki akses ke folder aplikasi. Keselamatan data tetap memerlukan izin filesystem yang tepat dan backup eksternal yang dikelola pengguna. Recovery mencoba mempertahankan database serta artefak untuk pemulihan, tetapi tidak menjamin pemulihan otomatis dari seluruh kegagalan media atau filesystem.

## Penanganan Data Uji

Seluruh reproduksi dan regression test wajib menggunakan database dan berkas fiktif pada direktori sementara terisolasi. Jangan menjalankan eksperimen keamanan terhadap database pengguna, folder UAT, backup operasional, atau release artifact yang sudah ada.
