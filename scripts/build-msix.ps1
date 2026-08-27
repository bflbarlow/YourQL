<#
.SYNOPSIS
  Build a signed MSIX package for YourQL on Windows.

.DESCRIPTION
  Packages the Wails-built YourQL.exe into a Desktop Bridge (runFullTrust)
  MSIX using the Windows SDK (makeappx.exe / signtool.exe), generates the
  required tile icon assets from build/appicon.png, and signs the result.

  Wails cannot produce MSIX directly; this script is the Windows half of the
  release build. Run it on a Windows machine with the Windows SDK installed.

.PARAMETER ExePath
  Path to the built YourQL.exe (default: ..\..\build\bin\YourQL.exe).

.PARAMETER Version
  4-part MSIX version (default 0.4.7.0). Must match AppxManifest.xml Identity.

.PARAMETER CertPath
  Path to a .pfx code-signing certificate. MSIX REQUIRES a signature; if you
  omit this, a local self-signed certificate is created for SIDELOADING ONLY
  (the package will not install on other machines without trusting that cert).

.PARAMETER CertPassword
  Password for the .pfx (optional).

.PARAMETER Publisher
  Publisher subject (CN=...) to stamp into the manifest; must match the cert.

.EXAMPLE
  .\build-msix.ps1 -CertPath C:\certs\yourql.pfx -CertPassword ********

.EXAMPLE
  .\build-msix.ps1   # self-signed, sideloading/dev only
#>
[CmdletBinding()]
param(
  [string]$ExePath = (Join-Path (Split-Path $PSScriptRoot -Parent) 'build\bin\YourQL.exe'),
  [string]$Version = '0.4.7.0',
  [string]$CertPath = '',
  [string]$CertPassword = '',
  [string]$Publisher = 'CN=YourQL'
)

$ErrorActionPreference = 'Stop'

# ── Locate Windows SDK tools ────────────────────────────────────────────────
function Find-SdkTool([string]$name) {
  $roots = @(
    "${env:ProgramFiles(x86)}\Windows Kits\10\bin",
    "$env:ProgramFiles\Windows Kits\10\bin"
  )
  foreach ($r in $roots) {
    if (Test-Path $r) {
      $found = Get-ChildItem -Path $r -Recurse -Filter "$name.exe" -ErrorAction SilentlyContinue |
               Sort-Object FullName -Descending | Select-Object -First 1
      if ($found) { return $found.FullName }
    }
  }
  throw "Could not find $name.exe. Install the Windows 10/11 SDK (makeappx + signtool)."
}

$makeappx = Find-SdkTool 'makeappx'
$signtool = Find-SdkTool 'signtool'

# ── Staging dirs ────────────────────────────────────────────────────────────
$stage = Join-Path $PSScriptRoot 'msix-stage'
if (Test-Path $stage) { Remove-Item $stage -Recurse -Force }
$assets = Join-Path $stage 'Assets'
New-Item -ItemType Directory -Path $assets -Force | Out-Null

# ── Copy exe + manifest ─────────────────────────────────────────────────────
if (-not (Test-Path $ExePath)) { throw "YourQL.exe not found at $ExePath. Run 'wails build' first." }
Copy-Item $ExePath (Join-Path $stage 'YourQL.exe')
Copy-Item (Join-Path (Split-Path $PSScriptRoot -Parent) 'packaging\msix\AppxManifest.xml') (Join-Path $stage 'AppxManifest.xml')

# ── Generate tile icons (System.Drawing) from build/appicon.png ─────────────
Add-Type -AssemblyName System.Drawing
$repoRoot = Split-Path $PSScriptRoot -Parent
$icon = Join-Path $repoRoot 'build\appicon.png'
if (-not (Test-Path $icon)) { throw "appicon.png not found at $icon" }

$sizes = @{
  'StoreLogo.png'          = 50
  'Square44x44Logo.png'    = 44
  'Square150x150Logo.png'  = 150
  'Wide310x150Logo.png'    = @(310, 150)
}
$src = [System.Drawing.Image]::FromFile($icon)
foreach ($entry in $sizes.GetEnumerator()) {
  $w = $entry.Value; $h = $entry.Value
  if ($entry.Value -is [array]) { $w = $entry.Value[0]; $h = $entry.Value[1] }
  $bmp = New-Object System.Drawing.Bitmap($w, $h)
  $g = [System.Drawing.Graphics]::FromImage($bmp)
  $g.InterpolationMode = [System.Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
  $g.DrawImage($src, 0, 0, $w, $h)
  $g.Dispose()
  $bmp.Save((Join-Path $assets $entry.Key), [System.Drawing.Imaging.ImageFormat]::Png)
  $bmp.Dispose()
}
$src.Dispose()

# ── Stamp version + publisher into the manifest ─────────────────────────────
$manifest = Join-Path $stage 'AppxManifest.xml'
(Get-Content $manifest -Raw) `
  -replace 'Version="[^"]*"', "Version=`"$Version`"" `
  -replace 'Publisher="[^"]*"', "Publisher=`"$Publisher`"" |
  Set-Content $manifest -NoNewline

# ── Signing identity ────────────────────────────────────────────────────────
if ($CertPath) {
  $cert = Get-PfxCertificate -FilePath $CertPath -Password (ConvertTo-SecureString $CertPassword -AsPlainText -Force)
  $thumb = $cert.Thumbprint
  Write-Host "Using certificate thumbprint: $thumb"
} else {
  Write-Warning "No -CertPath supplied: creating a self-signed cert for SIDELOADING ONLY."
  $cert = New-SelfSignedCertificate -Type CodeSigningCert -Subject $Publisher -CertStoreLocation Cert:\CurrentUser\My
  $thumb = $cert.Thumbprint
}

# ── Package ─────────────────────────────────────────────────────────────────
$msixName = "YourQL-$($Version -replace '\.0$','')-x64.msix"
$out = Join-Path $PSScriptRoot $msixName
if (Test-Path $out) { Remove-Item $out -Force }

& $makeappx pack /d $stage /p $out /o
if ($LASTEXITCODE -ne 0) { throw "makeappx failed with exit code $LASTEXITCODE" }

# ── Sign (mandatory for MSIX) ───────────────────────────────────────────────
if ($CertPath) {
  & $signtool sign /fd SHA256 /a /f $CertPath /p $CertPassword $out
} else {
  & $signtool sign /fd SHA256 /a /sha1 $thumb $out
}
if ($LASTEXITCODE -ne 0) { throw "signtool failed with exit code $LASTEXITCODE" }

Write-Host ""
Write-Host "MSIX built: $out"
Write-Host "Install:  Add-AppxPackage -Path `"$out`""
