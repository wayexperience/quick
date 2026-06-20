# Installer for the `quick` CLI on Windows. Downloads the prebuilt binary
# from the latest GitHub Release: no Go required.
#
#   irm https://<domain>/install.ps1 | iex
$ErrorActionPreference = "Stop"

$repo = "zupolgec/quick"

$arch = "amd64"
if ($env:PROCESSOR_ARCHITECTURE -eq "ARM64") { $arch = "arm64" }

$url = "https://github.com/$repo/releases/latest/download/quick_windows_$arch.zip"
$dir = Join-Path $env:LOCALAPPDATA "quick"
New-Item -ItemType Directory -Force -Path $dir | Out-Null

$zip = Join-Path $env:TEMP "quick.zip"
Write-Host "Downloading quick (windows/$arch)..."
Invoke-WebRequest -Uri $url -OutFile $zip
Expand-Archive -Path $zip -DestinationPath $dir -Force
Remove-Item $zip

Write-Host "quick installed in $dir\quick.exe"
if (($env:PATH -split ';') -notcontains $dir) {
  Write-Host "  Add $dir to your PATH (user):"
  Write-Host "    setx PATH `"$dir;`$env:PATH`""
}
Write-Host "  Then: quick login --server https://<your-domain>"
