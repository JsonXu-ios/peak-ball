# download_assets.ps1
# 从 stitch-html 中提取所有图片 URL 并下载到 public/images/
$ErrorActionPreference = "Continue"

$htmlDir  = "d:\JsonXu\vue_vuetify_V1\src\stitch-html"
$imgDir   = "d:\JsonXu\vue_vuetify_V1\public\images"

if (-not (Test-Path $imgDir)) { New-Item -ItemType Directory -Path $imgDir -Force }

# Collect all unique image URLs from HTML files
$allUrls = @()
Get-ChildItem -Path $htmlDir -Recurse -Filter "*.html" | ForEach-Object {
    $content = Get-Content $_.FullName -Raw
    $matches = [regex]::Matches($content, 'src="(https://lh3\.googleusercontent\.com/[^"]+)"')
    foreach ($m in $matches) {
        $allUrls += $m.Groups[1].Value
    }
}

$uniqueUrls = $allUrls | Sort-Object -Unique
Write-Host "Found $($uniqueUrls.Count) unique image URLs"

# Download each image
$idx = 1
foreach ($url in $uniqueUrls) {
    # Generate a short hash-based filename from URL
    $hash = [System.BitConverter]::ToString(
        [System.Security.Cryptography.MD5]::Create().ComputeHash(
            [System.Text.Encoding]::UTF8.GetBytes($url)
        )
    ).Replace("-","").Substring(0, 12).ToLower()
    $outFile = Join-Path $imgDir "img_${hash}.jpg"

    if (Test-Path $outFile) {
        Write-Host "[$idx/$($uniqueUrls.Count)] SKIP (exists): img_${hash}.jpg"
    } else {
        try {
            Invoke-WebRequest -Uri $url -OutFile $outFile -TimeoutSec 30 -ErrorAction Stop
            Write-Host "[$idx/$($uniqueUrls.Count)] OK: img_${hash}.jpg"
        } catch {
            Write-Host "[$idx/$($uniqueUrls.Count)] FAIL: img_${hash}.jpg - $($_.Exception.Message)"
        }
    }
    $idx++
}

# Also create a mapping file: hash -> original URL
$mapFile = Join-Path $imgDir "url_mapping.txt"
$idx = 0
$lines = @()
foreach ($url in $uniqueUrls) {
    $hash = [System.BitConverter]::ToString(
        [System.Security.Cryptography.MD5]::Create().ComputeHash(
            [System.Text.Encoding]::UTF8.GetBytes($url)
        )
    ).Replace("-","").Substring(0, 12).ToLower()
    $lines += "img_${hash}.jpg=$url"
}
$lines | Set-Content -Path $mapFile -Encoding UTF8
Write-Host "`nMapping saved to $mapFile"
Write-Host "Done!"
