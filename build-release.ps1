param(
    [Parameter(Mandatory=$true)]
    [string]$Version
)

if ($Version -notmatch '^v\d+\.\d+\.\d+$') {
    Write-Error "Version must be in format v1.0.0"
    exit 1
}

$buildDir = "build"
if (Test-Path $buildDir) {
    Remove-Item -Recurse -Force $buildDir
}
New-Item -ItemType Directory -Path $buildDir | Out-Null

Write-Host "Building phixgo $Version for Windows..." -ForegroundColor Green

$outputPath = "$buildDir\phixgo.exe"
& go build -o $outputPath .

if ($LASTEXITCODE -ne 0) {
    Write-Error "Build failed"
    exit 1
}

$zipName = "phixgo-$Version-windows-amd64.zip"
$zipPath = "$buildDir\$zipName"

Write-Host "Creating zip: $zipName" -ForegroundColor Cyan
Compress-Archive -Path $outputPath -DestinationPath $zipPath -Force

Remove-Item $outputPath

Write-Host "`nBuild complete!" -ForegroundColor Green
Get-ChildItem -Path $buildDir -Filter "*.zip" | ForEach-Object {
    $size = $_.Length / 1MB
    Write-Host "  $($_.Name) - $([math]::Round($size, 2)) MB" -ForegroundColor Yellow
}
