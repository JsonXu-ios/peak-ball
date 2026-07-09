# copy_named_images.ps1
# Copy hash-named images to meaningful names for Vue components
$imgDir = "d:\JsonXu\vue_vuetify_V1\public\images"

$copies = @{
    # Experts / Analysts
    "img_9a43ff82a55b.jpg" = "expert_marco.jpg"       # Professional male analyst portrait headshot (Marco Silva)
    "img_dacaf3ff8eb2.jpg" = "expert_elena.jpg"        # Female sports data scientist portrait (Elena Rossi)
    "img_d23547b7d366.jpg" = "expert_analyst.jpg"      # Portrait of a professional sports analyst

    # User Avatar
    "img_6b55811de655.jpg" = "avatar_alex.jpg"         # User Avatar (Alex)

    # Team Logos - Premier League
    "img_cdf6ff1c89ec.jpg" = "team_mancity.jpg"        # Manchester City Team Crest logo
    "img_5b539a358ac1.jpg" = "team_arsenal.jpg"        # Arsenal Team Crest logo
    "img_9df6d85b49c8.jpg" = "team_liverpool.jpg"      # Liverpool Team Crest logo
    "img_fe29aeff32ea.jpg" = "team_tottenham.jpg"      # Tottenham Team Crest logo
    "img_5d6b86559531.jpg" = "team_astonvilla.jpg"     # Aston Villa Team Crest logo
    "img_0ef424732b11.jpg" = "team_manutd.jpg"         # Manchester City... reuse for Man United (club crest)

    # Team Logos - other clubs
    "img_0e8e05e1d6dc.jpg" = "team_chelsea.jpg"        # Football club circular logo crest
    "img_36fe1bf32be5.jpg" = "team_realmadrid.jpg"     # Football team emblem on grass
    "img_37f097b1fc19.jpg" = "team_barcelona.jpg"      # FC Barcelona club crest logo
    "img_64a9717ea5dd.jpg" = "team_barcelona2.jpg"     # FC Barcelona football club crest (another)
    "img_8f256d2d8d4c.jpg" = "team_realmadrid2.jpg"    # Real Madrid football club crest
    "img_614a2b27ecf2.jpg" = "team_realmadrid3.jpg"    # Real Madrid club crest logo
    "img_dc986335f0fa.jpg" = "team_acmilan.jpg"        # Stylized soccer team logo illustration
    "img_5fa8c170566e.jpg" = "team_intermilan.jpg"     # Football club logo dark background
    "img_4f4d106b3b42.jpg" = "team_intermilan2.jpg"    # Football club logo white background

    # Generic team logos for match_list_home teams
    "img_6f70854cb63f.jpg" = "team_newcastle.jpg"
    "img_7ccd289f3a3b.jpg" = "team_westham.jpg"
    "img_9230f312d939.jpg" = "team_everton.jpg"
    "img_92f6d25d86df.jpg" = "team_fulham.jpg"
    "img_766cda4708ca.jpg" = "team_brighton.jpg"
    "img_14e6c8cf443d.jpg" = "team_burnley.jpg"

    # Match logos (Home/Away from match_analysis)
    "img_f990a87e2120.jpg" = "team_home.jpg"           # Home Team Logo
    "img_ce63c43a440e.jpg" = "team_away.jpg"           # Away Team Logo
    "img_84199e47bdd2.jpg" = "team_away2.jpg"
    "img_e4a068919b4a.jpg" = "team_away3.jpg"
    "img_783e09fa7427.jpg" = "team_home2.jpg"
    "img_8f7e354acfb8.jpg" = "team_home3.jpg"

    # Transfer club icons
    "img_42b89b9668d4.jpg" = "club_a.jpg"              # Club A
    "img_e661b2d152f9.jpg" = "club_b.jpg"              # Club B
    "img_6a5d0f1f54a2.jpg" = "club_c.jpg"              # Club C
    "img_8fb4c35061a2.jpg" = "club_d.jpg"              # Club D

    # Premier League Logo
    "img_4cc9a230a282.jpg" = "league_premier.jpg"      # Premier League Logo symbol

    # Player Photos
    "img_6ed947a21668.jpg" = "player_haaland.jpg"      # E. Haaland
    "img_5108e137aed9.jpg" = "player_salah.jpg"        # Mo Salah
    "img_df42d955390b.jpg" = "player_son.jpg"          # Son Heung-min

    # News Images
    "img_a1921c7c05ab.jpg" = "news_hero.jpg"           # Top Football Story
    "img_1e2a9df5a209.jpg" = "news_stadium.jpg"        # Stadium Atmosphere
    "img_00b46a3b5428.jpg" = "news_default.jpg"        # News item 1
    "img_ff8ee85129f1.jpg" = "news_featured1.jpg"      # News item 2
    "img_e05ddf3a1ed4.jpg" = "news_featured2.jpg"      # News item 3
    "img_618b6116751c.jpg" = "news_featured3.jpg"      # News item 4
    "img_5048ea79060c.jpg" = "news_official.jpg"       # Official News 1
    "img_5d9ea51b1ae0.jpg" = "news_official2.jpg"      # Official News 2
    "img_84227b984eed.jpg" = "news_soccer.jpg"         # Soccer Ball
}

$success = 0
$fail = 0
foreach ($kv in $copies.GetEnumerator()) {
    $src = Join-Path $imgDir $kv.Key
    $dst = Join-Path $imgDir $kv.Value
    if (Test-Path $src) {
        Copy-Item $src $dst -Force
        $success++
    } else {
        Write-Host "MISSING: $($kv.Key)"
        $fail++
    }
}
Write-Host "Copied $success images, $fail failed"
Write-Host ""

# List final images
$count = (Get-ChildItem $imgDir -File).Count
Write-Host "Total files in images dir: $count"
