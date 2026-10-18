<#
.SYNOPSIS
    Wrapper build script untuk menghasilkan rilis portable Compliance Workspace v1.1.0 Windows x64.
.DESCRIPTION
    Menjalankan kompilasi Windows x64 berikon resmi (build-windows.ps1) dan
    menyiapkan paket distribusi zip portabel (package-release.ps1).
#>
param(
    [switch]$SkipTests = $false
)

$ErrorActionPreference = "Stop"

$workspaceRoot = (Get-Item -Path $PSScriptRoot).Parent.FullName
$buildScript = Join-Path $PSScriptRoot "build-windows.ps1"
$packageScript = Join-Path $PSScriptRoot "package-release.ps1"

Write-Host "==========================================================" -ForegroundColor Cyan
Write-Host " Compliance Workspace v1.1.0 -- Unified Portable Build    " -ForegroundColor Cyan
Write-Host "==========================================================" -ForegroundColor Cyan

# 1. Jalankan build executable dengan resource Windows
& $buildScript

# 2. Kemas paket distribusi rilis
& $packageScript
