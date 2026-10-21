<#
.SYNOPSIS
    Regression test terisolasi untuk preflight dan packaging v1.1.1.
.PARAMETER ValidExecutablePath
    Executable v1.1.1 yang sudah diverifikasi untuk skenario positif.
.PARAMETER LegacyExecutablePath
    Executable versi lama yang harus ditolak.
#>
param(
    [string]$ValidExecutablePath,
    [string]$LegacyExecutablePath
)

$ErrorActionPreference = "Stop"

if (-not (Test-Path -LiteralPath $ValidExecutablePath -PathType Leaf)) {
    throw "Executable valid tidak ditemukan: '$ValidExecutablePath'."
}
if (-not (Test-Path -LiteralPath $LegacyExecutablePath -PathType Leaf)) {
    throw "Executable legacy tidak ditemukan: '$LegacyExecutablePath'."
}

$sourcePackageScript = Join-Path $PSScriptRoot "package-release.ps1"
$sourceReadme = Join-Path $PSScriptRoot "README.txt"
$sourceVersion = Join-Path (Split-Path -Parent $PSScriptRoot) "VERSION.txt"
$tempRoot = Join-Path ([System.IO.Path]::GetTempPath()) ("cw-package-regression-" + [guid]::NewGuid().ToString("N"))

function New-TestRepository {
    param([string]$Name)

    $root = Join-Path $tempRoot $Name
    $scriptsDir = Join-Path $root "scripts"
    New-Item -ItemType Directory -Path $scriptsDir | Out-Null
    Copy-Item -LiteralPath $sourcePackageScript -Destination (Join-Path $scriptsDir "package-release.ps1")
    Copy-Item -LiteralPath $sourceReadme -Destination (Join-Path $scriptsDir "README.txt")
    Copy-Item -LiteralPath $sourceVersion -Destination (Join-Path $root "VERSION.txt")
    return $root
}

function Invoke-PackageProcess {
    param(
        [string]$RepositoryRoot,
        [string]$ExecutablePath
    )

    $scriptPath = Join-Path $RepositoryRoot "scripts\package-release.ps1"
    $startInfo = New-Object System.Diagnostics.ProcessStartInfo
    $startInfo.FileName = (Get-Command powershell.exe).Source
    $startInfo.Arguments = "-NoProfile -ExecutionPolicy Bypass -File `"$scriptPath`" -ExecutablePath `"$ExecutablePath`""
    $startInfo.UseShellExecute = $false
    $startInfo.RedirectStandardOutput = $true
    $startInfo.RedirectStandardError = $true

    $process = New-Object System.Diagnostics.Process
    $process.StartInfo = $startInfo
    $null = $process.Start()
    $standardOutput = $process.StandardOutput.ReadToEnd()
    $standardError = $process.StandardError.ReadToEnd()
    $process.WaitForExit()

    return [pscustomobject]@{
        ExitCode = $process.ExitCode
        Output = $standardOutput + $standardError
    }
}

try {
    New-Item -ItemType Directory -Path $tempRoot | Out-Null

    $missingRoot = New-TestRepository -Name "missing-source"
    $missingPath = Join-Path $missingRoot "not-found.exe"
    $missingResult = Invoke-PackageProcess -RepositoryRoot $missingRoot -ExecutablePath $missingPath
    if ($missingResult.ExitCode -eq 0 -or $missingResult.Output -notmatch "tidak ditemukan atau bukan file") {
        throw "FAIL: jalur sumber yang tidak tersedia tidak ditolak secara aman.`n$($missingResult.Output)"
    }
    if (Test-Path -LiteralPath (Join-Path $missingRoot "release")) {
        throw "FAIL: preflight sumber hilang membuat direktori release."
    }
    Write-Host "PASS missing source rejected (exit $($missingResult.ExitCode))"

    $legacyRoot = New-TestRepository -Name "legacy-source"
    $existingPackage = Join-Path $legacyRoot "release\ComplianceWorkspace-v1.1.1-Windows-x64"
    New-Item -ItemType Directory -Path $existingPackage | Out-Null
    $sentinelPath = Join-Path $existingPackage "sentinel.txt"
    Set-Content -LiteralPath $sentinelPath -Value "do-not-modify" -Encoding ascii
    $sentinelHashBefore = (Get-FileHash -LiteralPath $sentinelPath -Algorithm SHA256).Hash

    $legacyResult = Invoke-PackageProcess -RepositoryRoot $legacyRoot -ExecutablePath $LegacyExecutablePath
    if ($legacyResult.ExitCode -eq 0 -or $legacyResult.Output -notmatch "FileVersion.*tidak sesuai") {
        throw "FAIL: executable versi lama tidak ditolak karena FileVersion.`n$($legacyResult.Output)"
    }
    $sentinelHashAfter = (Get-FileHash -LiteralPath $sentinelPath -Algorithm SHA256).Hash
    if ($sentinelHashBefore -ne $sentinelHashAfter) {
        throw "FAIL: output release yang sudah ada berubah saat preflight gagal."
    }
    if (Test-Path -LiteralPath (Join-Path $legacyRoot "release\ComplianceWorkspace-v1.1.1-Windows-x64.zip")) {
        throw "FAIL: preflight versi lama membuat paket ZIP."
    }
    Write-Host "PASS legacy version rejected before existing release changed (exit $($legacyResult.ExitCode))"
    Write-Host "PASS rejection reason contains: FileVersion '1.1.0.0' tidak sesuai dengan versi target '1.1.1.0'"
    Write-Host "PASS existing release sentinel SHA-256 unchanged: $sentinelHashAfter"

    $validRoot = New-TestRepository -Name "valid-source"
    $validResult = Invoke-PackageProcess -RepositoryRoot $validRoot -ExecutablePath $ValidExecutablePath
    if ($validResult.ExitCode -ne 0) {
        throw "FAIL: executable v1.1.1 yang valid ditolak.`n$($validResult.Output)"
    }

    $packagedExe = Join-Path $validRoot "release\ComplianceWorkspace-v1.1.1-Windows-x64\ComplianceWorkspace.exe"
    $packageZip = Join-Path $validRoot "release\ComplianceWorkspace-v1.1.1-Windows-x64.zip"
    $checksums = Join-Path $validRoot "release\checksums-v1.1.1-sha256.txt"
    foreach ($path in @($packagedExe, $packageZip, $checksums)) {
        if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
            throw "FAIL: output packaging tidak ditemukan: '$path'."
        }
    }

    $validHash = (Get-FileHash -LiteralPath $ValidExecutablePath -Algorithm SHA256).Hash
    $packagedHash = (Get-FileHash -LiteralPath $packagedExe -Algorithm SHA256).Hash
    $zipHash = (Get-FileHash -LiteralPath $packageZip -Algorithm SHA256).Hash
    if ($validHash -ne $packagedHash) {
        throw "FAIL: checksum executable sumber dan paket berbeda."
    }
    $checksumText = Get-Content -LiteralPath $checksums -Raw
    if ($checksumText -notmatch [regex]::Escape($packagedHash) -or $checksumText -notmatch [regex]::Escape($zipHash)) {
        throw "FAIL: berkas checksum tidak memuat checksum executable dan ZIP."
    }
    Write-Host "PASS valid v1.1.1 source packaged in isolated temporary repository (exit $($validResult.ExitCode))"
    Write-Host "PASS executable SHA-256: $packagedHash"
    Write-Host "PASS package ZIP SHA-256: $zipHash"

    $checksumsHash = (Get-FileHash -LiteralPath $checksums -Algorithm SHA256).Hash
    $repeatResult = Invoke-PackageProcess -RepositoryRoot $validRoot -ExecutablePath $ValidExecutablePath
    if ($repeatResult.ExitCode -eq 0 -or $repeatResult.Output -notmatch "agar tidak menimpa output yang sudah ada") {
        throw "FAIL: packaging kedua tidak menolak output yang sudah ada.`n$($repeatResult.Output)"
    }
    if ((Get-FileHash -LiteralPath $packagedExe -Algorithm SHA256).Hash -ne $packagedHash -or
        (Get-FileHash -LiteralPath $packageZip -Algorithm SHA256).Hash -ne $zipHash -or
        (Get-FileHash -LiteralPath $checksums -Algorithm SHA256).Hash -ne $checksumsHash) {
        throw "FAIL: packaging kedua mengubah output yang sudah ada."
    }
    Write-Host "PASS existing package, ZIP, and checksum refused without modification (exit $($repeatResult.ExitCode))"
} finally {
    $resolvedTempBase = [System.IO.Path]::GetFullPath([System.IO.Path]::GetTempPath())
    $resolvedTempRoot = [System.IO.Path]::GetFullPath($tempRoot)
    if ($resolvedTempRoot.StartsWith($resolvedTempBase, [System.StringComparison]::OrdinalIgnoreCase) -and
        (Split-Path -Leaf $resolvedTempRoot) -like "cw-package-regression-*") {
        Remove-Item -LiteralPath $resolvedTempRoot -Recurse -Force -ErrorAction SilentlyContinue
    }
}
