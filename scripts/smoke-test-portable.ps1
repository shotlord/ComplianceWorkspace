<#
.SYNOPSIS
    Wrapper smoke test untuk Compliance Workspace v1.1.0 Windows x64.
.DESCRIPTION
    Menjalankan pengujian 14 skenario portabilitas dan integritas melalui run-smoke-test.ps1.
#>

$ErrorActionPreference = "Stop"

$smokeScript = Join-Path $PSScriptRoot "run-smoke-test.ps1"
& $smokeScript
