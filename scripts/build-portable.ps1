<#
.SYNOPSIS
    Wrapper build script untuk menghasilkan rilis portable Compliance Workspace v1.1.1 Windows x64.
.DESCRIPTION
    Menjalankan kompilasi Windows x64 berikon resmi (build-windows.ps1) dan
    menyiapkan paket distribusi zip portabel (package-release.ps1).
.PARAMETER ExecutablePath
    Jalur output build baru yang juga akan dipakai sebagai sumber packaging.
#>
param(
    [string]$ExecutablePath,
    [switch]$SkipTests = $false
)

$ErrorActionPreference = "Stop"

if ([string]::IsNullOrWhiteSpace($ExecutablePath)) {
    throw "Parameter -ExecutablePath wajib diisi dengan jalur build baru yang terisolasi."
}

$workspaceRoot = (Get-Item -Path $PSScriptRoot).Parent.FullName
$buildScript = Join-Path $PSScriptRoot "build-windows.ps1"
$packageScript = Join-Path $PSScriptRoot "package-release.ps1"

Write-Host "==========================================================" -ForegroundColor Cyan
Write-Host " Compliance Workspace v1.1.1 -- Unified Portable Build    " -ForegroundColor Cyan
Write-Host "==========================================================" -ForegroundColor Cyan

# 1. Jalankan build executable dengan resource Windows
& $buildScript -OutputPath $ExecutablePath -SkipTests:$SkipTests

# 2. Kemas paket distribusi rilis
& $packageScript -ExecutablePath $ExecutablePath
