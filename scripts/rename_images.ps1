# rename_images.ps1
# 从 stitch-html 中提取图片上下文，将 hash 文件复制为有意义名称
$ErrorActionPreference = "Continue"
$htmlDir = "d:\JsonXu\vue_vuetify_V1\src\stitch-html"
$imgDir  = "d:\JsonXu\vue_vuetify_V1\public\images"

# Build URL -> hash filename lookup
$urlToHash = @{}
Get-Content "$imgDir\url_mapping.txt" | ForEach-Object {
    $parts = $_ -split "=", 2
    if ($parts.Count -eq 2) { $urlToHash[$parts[1]] = $parts[0] }
}

# Build URL -> alt text lookup from ALL HTML files
$urlToAlt = @{}
Get-ChildItem -Path $htmlDir -Recurse -Filter "*.html" | ForEach-Object {
    $c = Get-Content $_.FullName -Raw
    # Pattern: alt="..." ... src="https://lh3..."
    $ms1 = [regex]::Matches($c, 'alt="([^"]*)"[^>]*?src="(https://lh3\.googleusercontent\.com/[^"]+)"')
    foreach ($m in $ms1) {
        $url = $m.Groups[2].Value
        $alt = $m.Groups[1].Value
        if (-not $urlToAlt.ContainsKey($url) -and $alt) { $urlToAlt[$url] = $alt }
    }
    # Pattern: src="https://lh3..." ... alt="..."
    $ms2 = [regex]::Matches($c, 'src="(https://lh3\.googleusercontent\.com/[^"]+)"[^>]*?alt="([^"]*)"')
    foreach ($m in $ms2) {
        $url = $m.Groups[1].Value
        $alt = $m.Groups[2].Value
        if (-not $urlToAlt.ContainsKey($url) -and $alt) { $urlToAlt[$url] = $alt }
    }
}

Write-Host "URL->Hash entries: $($urlToHash.Count)"
Write-Host "URL->Alt entries: $($urlToAlt.Count)"
Write-Host ""

# Print alt -> hash mapping
foreach ($url in $urlToAlt.Keys) {
    $hash = $urlToHash[$url]
    $alt = $urlToAlt[$url]
    if ($hash) {
        Write-Host "$hash | $alt"
    }
}
