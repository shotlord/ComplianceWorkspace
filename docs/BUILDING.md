# Panduan Kompilasi & Pembuatan Paket Rilis (Building)

Dokumen ini menjelaskan prosedur kompilasi biner mandiri (*single executable binary*) dan pembuatan paket distribusi portabel untuk **Compliance Workspace v1.1.0** pada lingkungan Windows x64.

---

## 1. Perbedaan Source Repository vs Binary Release

| Karakteristik | Source Repository (Git) | Binary Release Artifacts (Zip) |
| :--- | :--- | :--- |
| **Tujuan** | Pengembangan, audit kode, dan pengujian otomatis. | Penggunaan langsung oleh Compliance Officer tanpa instalasi alat pengembangan. |
| **Isi** | Source code Go, web embedded assets, skrip build, file migrasi SQL, unit & regression tests. | Satu file `ComplianceWorkspace.exe`, folder penyimpanan `data/`, `backups/`, `exports/`, `README.txt`, dan `VERSION.txt`. |
| **Ketergantungan** | Go toolchain 1.22+ dan PowerShell. | **Nol ketergantungan runtime (Zero-dependency)**; tidak memerlukan Go, Node.js, atau database server. |
| **Penyimpanan Git** | Terlacak di Git. | Dikecualikan dari Git melalui `.gitignore` (folder `release/` dan `uat/`). |

---

## 2. Prasyarat Lingkungan Pengembangan

Untuk melakukan build dari *clean clone*:

1. **Sistem Operasi**: Windows 10 atau Windows 11 (64-bit / x64).
2. **Go Toolchain**: Go versi 1.22 atau lebih baru.
   ```powershell
   go version
   ```
3. **PowerShell**: Windows PowerShell 5.1 atau PowerShell 7+.
4. **Alat Windows Resource (go-winres)** *(Opsional / Otomatis)*:
   Skrip build akan otomatis mendeteksi atau memasang `github.com/tc-hib/go-winres@v0.3.3` jika belum tersedia di sistem.
5. **Node.js** *(Opsional)*:
   Digunakan untuk validasi sintaks JavaScript frontend (`node --check`) sebelum proses kompilasi.

---

## 3. Alur Kompilasi Biner Windows AMD64

Jalur build utama menggunakan skrip otomatis yang telah teruji:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\build-windows.ps1
```

### Tahapan yang Dijalankan oleh Skrip:
1. **Verifikasi Lingkungan**: Memeriksa Go toolchain dan memverifikasi keberadaan aset ikon resmi (`assets/branding/ComplianceWorkspace_app.ico`).
2. **Kompilasi Windows Resource PE**: Menjalankan `go-winres simply` dengan target arsitektur `amd64`, menyematkan ikon multi-resolusi (16x16 hingga 256x256 px), manifest CLI, serta metadata versi (`1.1.0.0`).
3. **Validasi Kualitas Kode**: Menjalankan pemeriksaan sintaksis JavaScript (`node --check`), pemeriksaan statis Go (`go vet ./...`), dan seluruh pengujian otomatis (`go test -count=1 ./...`).
4. **Kompilasi Go Pure-Go**: Mengompilasi aplikasi dengan `CGO_ENABLED=0`, `GOOS=windows`, `GOARCH=amd64`, serta *linker flags* optimasi `-ldflags "-s -w"` untuk menghasilkan executable mandiri tanpa ketergantungan pustaka C runtime eksternal.
5. **Verifikasi Integritas Biner**: Memeriksa metadata Windows PE dan menghitung nilai checksum SHA-256 pada biner output (`uat/compliance-workspace.exe`).

---

## 4. Pembuatan Paket Rilis Portabel (Packaging)

Setelah biner berhasil dikompilasi, jalankan skrip pengemasan rilis:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\package-release.ps1
```

Skrip ini akan:
1. Menyiapkan struktur folder portabel di `release/ComplianceWorkspace-v1.1.0-Windows-x64/` lengkap dengan subfolder `data/`, `backups/`, dan `exports/`.
2. Menyalin biner `ComplianceWorkspace.exe`, petunjuk pengguna `README.txt`, dan informasi versi `VERSION.txt`.
3. Mengompres direktori menjadi arsip `ComplianceWorkspace-v1.1.0-Windows-x64.zip`.
4. Menghasilkan berkas verifikasi checksum `release/checksums-sha256.txt`.

---

## 5. Kompilasi Manual Sederhana (Untuk Keperluan Debugging)

Jika pengembang ingin melakukan kompilasi cepat tanpa langkah packaging dan ikon Windows:

```powershell
$env:CGO_ENABLED = "0"
$env:GOOS = "windows"
$env:GOARCH = "amd64"
go build -trimpath -o compliance-workspace.exe ./cmd/compliance-workspace
```

Atau langsung menjalankan aplikasi dalam mode pengembangan:

```powershell
go run ./cmd/compliance-workspace
```

Aplikasi akan otomatis mengikat port loopback yang tersedia dan menampilkan URL akses lokal pada konsol.
