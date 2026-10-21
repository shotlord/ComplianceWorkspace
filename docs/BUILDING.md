# Panduan Kompilasi & Pembuatan Paket Rilis (Building)

Dokumen ini menjelaskan build Windows x64 dan packaging portabel **Compliance Workspace v1.1.1**. Versi ini masih berstatus `Unreleased`; official build dan packaging hanya dilakukan setelah source freeze serta otorisasi Project Owner.

---

## 1. Source Repository dan Binary Release

| Karakteristik | Source Repository (Git) | Binary Release Artifacts (ZIP) |
| :--- | :--- | :--- |
| **Tujuan** | Pengembangan, audit kode, dan pengujian. | Penggunaan langsung tanpa alat pengembangan. |
| **Isi** | Source Go, embedded web assets, migrasi, skrip, dan tests. | `ComplianceWorkspace.exe`, `README.txt`, `VERSION.txt`, serta folder `data/`, `backups/`, dan `exports/`. |
| **Ketergantungan** | Go 1.27.2, PowerShell, dependency module Go, dan `go-winres` v0.3.3. | Tidak memerlukan Go, Node.js, CGO, atau database server saat runtime. |
| **Penyimpanan Git** | Source dilacak Git. | `release/` dan `uat/` dikecualikan melalui `.gitignore`. |

`VERSION.txt` mencatat metadata rilis aktif: versi aplikasi `1.1.1`, schema aktif 6, serta build Windows amd64 dengan `CGO_ENABLED=0`. Skrip build/package saat ini juga menetapkan target v1.1.1 dan PE `1.1.1.0` secara eksplisit; quality gate harus memastikan nilai-nilai tersebut tetap konsisten karena PE version tidak diturunkan otomatis dari `VERSION.txt`.

---

## 2. Prasyarat Build

1. Windows 10/11 x64.
2. Go **1.27.2**, sesuai directive `go` dalam `go.mod` dan `BUILD_TOOLCHAIN` pada `VERSION.txt`.
3. Windows PowerShell 5.1 atau PowerShell 7+.
4. `github.com/tc-hib/go-winres@v0.3.3`. `build-windows.ps1` memakai tool yang sudah ada atau menjalankan `go install` bila belum tersedia; instalasi awal ini memerlukan akses ke module source.
5. Node.js opsional. Jika tersedia dan tests tidak dilewati, skrip menjalankan `node --check` terhadap JavaScript frontend.
6. Dependency Go pada `go.mod`/`go.sum` harus tersedia di module cache atau dapat diunduh pada lingkungan build. Kebutuhan jaringan saat menyiapkan toolchain tidak mengubah sifat offline aplikasi saat runtime.

---

## 3. Build Windows AMD64

Gunakan direktori output baru di luar `release/` agar executable lama tidak tertimpa:

```powershell
$buildDir = Join-Path $env:TEMP ("ComplianceWorkspace-v1.1.1-build-" + (Get-Date -Format "yyyyMMdd-HHmmss"))
New-Item -ItemType Directory -Path $buildDir | Out-Null
$executablePath = Join-Path $buildDir "ComplianceWorkspace-v1.1.1-Windows-x64.exe"
powershell -ExecutionPolicy Bypass -File .\scripts\build-windows.ps1 -OutputPath $executablePath
```

Parameter `-OutputPath` wajib. Skrip:

1. memeriksa Go dan ikon `assets/branding/ComplianceWorkspace_app.ico`;
2. membentuk `cmd/compliance-workspace/rsrc_windows_amd64.syso` dengan ikon, manifest, `ProductName`, `FileVersion=1.1.1.0`, dan `ProductVersion=1.1.1.0`;
3. menjalankan `node --check` jika Node.js tersedia, lalu `go vet ./...` dan `go test -count=1 ./...`;
4. membangun `GOOS=windows`, `GOARCH=amd64`, `CGO_ENABLED=0` menggunakan `-trimpath` dan `-ldflags="-s -w"`;
5. memverifikasi metadata PE, ekstraksi ikon Windows Shell, dan SHA-256 executable.

`-SkipTests` melewati langkah 3 saja; resource, kompilasi, pemeriksaan metadata PE, ikon, dan checksum tetap dijalankan. Jangan gunakan `-SkipTests` untuk official build kecuali hasil pengujian lengkap dari source/commit yang sama sudah direkam secara terpisah dan penggunaannya disetujui.

Build manual berikut hanya untuk debugging. Hasilnya tidak memuat kontrak resource/metadata yang diverifikasi skrip dan bukan sumber packaging resmi:

```powershell
$env:CGO_ENABLED = "0"
$env:GOOS = "windows"
$env:GOARCH = "amd64"
go build -trimpath -o compliance-workspace.exe ./cmd/compliance-workspace
```

---

## 4. Build dan Packaging Terpadu

`build-portable.ps1` meneruskan jalur yang sama ke build dan packaging:

```powershell
$buildDir = Join-Path $env:TEMP ("ComplianceWorkspace-v1.1.1-portable-" + (Get-Date -Format "yyyyMMdd-HHmmss"))
New-Item -ItemType Directory -Path $buildDir | Out-Null
$executablePath = Join-Path $buildDir "ComplianceWorkspace-v1.1.1-Windows-x64.exe"
powershell -ExecutionPolicy Bypass -File .\scripts\build-portable.ps1 -ExecutablePath $executablePath
```

Parameter `-ExecutablePath` wajib dan menjadi output build sekaligus sumber package. Switch `-SkipTests` diteruskan ke `build-windows.ps1` dengan konsekuensi yang dijelaskan pada bagian 3.

---

## 5. Packaging dari Executable Terpilih

Setelah build yang diotorisasi selesai, executable harus dipilih secara eksplisit:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\package-release.ps1 -ExecutablePath $executablePath
```

Sebelum membuat atau mengubah direktori `release/`, preflight `package-release.ps1`:

1. menolak jalur kosong, tidak ada, bukan file, tidak terbaca, symbolic link, atau reparse point;
2. mewajibkan `ProductName=Compliance Workspace`, `FileVersion=1.1.1.0`, dan `ProductVersion=1.1.1.0`; executable v1.1.0 ditolak;
3. mencocokkan `VERSION=1.1.1` pada `VERSION.txt`;
4. menghitung SHA-256 sumber;
5. menghentikan proses jika direktori package, ZIP, atau checksum target sudah ada—tanpa menghapus atau menimpanya.

Jika preflight lolos, skrip menghasilkan:

- `release/ComplianceWorkspace-v1.1.1-Windows-x64/`;
- `release/ComplianceWorkspace-v1.1.1-Windows-x64.zip`;
- `release/checksums-v1.1.1-sha256.txt`, berisi SHA-256 executable dalam package dan ZIP.

Executable dalam package diverifikasi memiliki hash yang sama dengan sumber. Skrip tidak memilih executable dari `uat/` atau lokasi legacy secara otomatis.

---

## 6. Provenance Official Build

Skrip memverifikasi executable dan versi, tetapi tidak menyematkan atau membuktikan Git commit secara otomatis. Untuk official build setelah source freeze:

1. catat branch dan full commit hash dengan `git branch --show-current` dan `git rev-parse HEAD`;
2. pastikan working tree sesuai snapshot/source freeze yang disetujui;
3. jalankan quality gate pada source tersebut;
4. build ke direktori temporer baru dengan `-OutputPath` atau `-ExecutablePath` eksplisit;
5. catat output metadata PE dan SHA-256;
6. lakukan packaging hanya setelah otorisasi, lalu catat SHA-256 executable serta ZIP dari checksum version-specific.

`BUILD_DATE=2026-10-20` pada `VERSION.txt` adalah metadata build yang saat ini disetujui dan tidak diubah oleh skrip. Kesesuaiannya dengan tanggal official release harus diputuskan Project Owner sebelum source freeze; dokumentasi ini tidak menetapkan tanggal rilis.

---

## 7. Menjalankan dari Source

```powershell
go run ./cmd/compliance-workspace
```

Aplikasi mengikat port loopback yang tersedia dan menampilkan URL `http://127.0.0.1:<port>` pada konsol. Buka URL tersebut secara manual di peramban.
