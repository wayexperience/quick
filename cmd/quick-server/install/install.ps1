# Installer della CLI `quick` per Windows. Scarica il binario gia compilato
# dall'ultima GitHub Release: nessun Go richiesto.
#
#   irm https://<dominio>/install.ps1 | iex
$ErrorActionPreference = "Stop"

$repo = "zupolgec/quick"

$arch = "amd64"
if ($env:PROCESSOR_ARCHITECTURE -eq "ARM64") { $arch = "arm64" }

$url = "https://github.com/$repo/releases/latest/download/quick_windows_$arch.zip"
$dir = Join-Path $env:LOCALAPPDATA "quick"
New-Item -ItemType Directory -Force -Path $dir | Out-Null

$zip = Join-Path $env:TEMP "quick.zip"
Write-Host "Scarico quick (windows/$arch)..."
Invoke-WebRequest -Uri $url -OutFile $zip
Expand-Archive -Path $zip -DestinationPath $dir -Force
Remove-Item $zip

Write-Host "quick installato in $dir\quick.exe"
if (($env:PATH -split ';') -notcontains $dir) {
  Write-Host "  Aggiungi $dir al PATH (utente):"
  Write-Host "    setx PATH `"$dir;`$env:PATH`""
}
Write-Host "  Poi: quick login --server https://<il-tuo-dominio>"
