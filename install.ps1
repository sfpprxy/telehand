$ErrorActionPreference = "Stop"

$repo = "sfpprxy/telehand"
$binaryName = "telehand.exe"

# Detect arch
$arch = if ([Environment]::Is64BitOperatingSystem) { "amd64" } else { "386" }

# Get latest version
try {
    $release = Invoke-RestMethod -Uri "https://api.github.com/repos/$repo/releases/latest" -UseBasicParsing
    $version = $release.tag_name
} catch {
    Write-Error "Failed to get latest version"
    exit 1
}

$filename = "telehand-windows-$arch-$version.zip"

$urls = @(
    "https://github.com/$repo/releases/download/$version/$filename",
    "https://ghfast.top/https://github.com/$repo/releases/download/$version/$filename"
)

Write-Host "Installing telehand $version (windows/$arch)..."

$downloaded = $false
foreach ($url in $urls) {
    Write-Host "Trying: $url"
    try {
        Invoke-WebRequest -Uri $url -OutFile $filename -UseBasicParsing
        $downloaded = $true
        break
    } catch {
        Write-Host "  Failed, trying next source..."
    }
}

if (-not $downloaded) {
    Write-Error "Failed to download from all sources"
    exit 1
}

Expand-Archive -Path $filename -DestinationPath . -Force
Remove-Item $filename -Force

Write-Host ""
Write-Host "Installed telehand $version to $(Get-Location)\$binaryName"
Write-Host "Run '.\$binaryName' to start."
