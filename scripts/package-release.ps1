$ErrorActionPreference = "Stop"

$workspaceRoot = $PSScriptRoot | Split-Path -Parent
$releaseDir = Join-Path $workspaceRoot "release"
$pkgDir = Join-Path $releaseDir "ComplianceWorkspace-v1.1.0-Windows-x64"
$zipPath = Join-Path $releaseDir "ComplianceWorkspace-v1.1.0-Windows-x64.zip"
$srcExe = Join-Path $workspaceRoot "uat\compliance-workspace.exe"
$dstExe = Join-Path $pkgDir "ComplianceWorkspace.exe"

Write-Host "Copying fresh executable to release directory..."
Copy-Item $srcExe $dstExe -Force

Write-Host "Compressing release package to ZIP..."
if (Test-Path $zipPath) {
    Remove-Item $zipPath -Force
}
Compress-Archive -Path "$pkgDir\*" -DestinationPath $zipPath -Force

Write-Host "Calculating SHA-256 hashes..."
$exeHash = (Get-FileHash $dstExe -Algorithm SHA256).Hash
$zipHash = (Get-FileHash $zipPath -Algorithm SHA256).Hash

$lines = @(
    "$exeHash *ComplianceWorkspace.exe",
    "$zipHash *ComplianceWorkspace-v1.1.0-Windows-x64.zip"
)
$checksumPath = Join-Path $releaseDir "checksums-sha256.txt"
Set-Content -Path $checksumPath -Value $lines -Encoding utf8

Write-Host "Done!"
Write-Host "Executable Hash: $exeHash"
Write-Host "ZIP Hash:        $zipHash"
