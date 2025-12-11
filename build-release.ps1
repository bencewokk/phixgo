# Build and package phixgo for multiple platforms
param(
    [Parameter(Mandatory=$true)]
    [string]$Version
)

# Validate version format
if ($Version -notmatch '^v\d+\.\d+\.\d+$') {
    Write-Host "Error: Version must be in format v1.0.0" -ForegroundColor Red
    exit 1
}

Write-Host "Building phixgo $Version for multiple platforms..." -ForegroundColor Green

# Create build directory
$buildDir = "build"
if (Test-Path $buildDir) {
    Remove-Item -Recurse -Force $buildDir
}
New-Item -ItemType Directory -Path $buildDir | Out-Null

# Define platforms to build
$platforms = @(
    @{GOOS="windows"; GOARCH="amd64"; EXT=".exe"},
    @{GOOS="windows"; GOARCH="arm64"; EXT=".exe"},
    @{GOOS="linux"; GOARCH="amd64"; EXT=""},
    @{GOOS="linux"; GOARCH="arm64"; EXT=""},
    @{GOOS="darwin"; GOARCH="amd64"; EXT=""},
    @{GOOS="darwin"; GOARCH="arm64"; EXT=""}
)

foreach ($platform in $platforms) {
    $os = $platform.GOOS
    $arch = $platform.GOARCH
    $ext = $platform.EXT
    
    $binaryName = "phixgo$ext"
    $zipName = "phixgo-$Version-$os-$arch.zip"
    $platformDir = Join-Path $buildDir "$os-$arch"
    
    Write-Host "`nBuilding for $os-$arch..." -ForegroundColor Cyan
    
    # Create platform directory
    New-Item -ItemType Directory -Path $platformDir -Force | Out-Null
    
    # Set environment variables and build
    $env:GOOS = $os
    $env:GOARCH = $arch
    $env:CGO_ENABLED = "0"
    
    $outputPath = Join-Path $platformDir $binaryName
    
    try {
        go build -ldflags "-s -w" -o $outputPath .
        
        if ($LASTEXITCODE -eq 0) {
            Write-Host "  ✓ Built successfully" -ForegroundColor Green
            
            # Create zip archive
            $zipPath = Join-Path $buildDir $zipName
            Compress-Archive -Path $outputPath -DestinationPath $zipPath -Force
            Write-Host "  ✓ Created $zipName" -ForegroundColor Green
            
            # Show file size
            $size = (Get-Item $zipPath).Length / 1MB
            Write-Host "  Size: $([math]::Round($size, 2)) MB" -ForegroundColor Gray
        } else {
            Write-Host "  ✗ Build failed" -ForegroundColor Red
        }
    } catch {
        Write-Host "  ✗ Build failed: $_" -ForegroundColor Red
    }
}

# Clean up platform directories, keep only zip files
Get-ChildItem -Path $buildDir -Directory | Remove-Item -Recurse -Force

Write-Host "`n========================================" -ForegroundColor Green
Write-Host "Build complete! Release files are in the 'build' directory:" -ForegroundColor Green
Get-ChildItem -Path $buildDir -Filter "*.zip" | ForEach-Object {
    Write-Host "  - $($_.Name)" -ForegroundColor Yellow
}

Write-Host "`nNext steps:" -ForegroundColor Cyan
Write-Host "1. Go to: https://github.com/bencewokk/phixgo/releases/new" -ForegroundColor White
Write-Host "2. Create a new release with tag: $Version" -ForegroundColor White
Write-Host "3. Upload all zip files from the build directory" -ForegroundColor White
Write-Host "4. Publish the release" -ForegroundColor White
Write-Host "`nUsers can then run: phixgo --update" -ForegroundColor Green
