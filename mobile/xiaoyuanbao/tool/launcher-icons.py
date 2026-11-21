"""Generate Android density and adaptive icons from our original vector render."""
from pathlib import Path
from PIL import Image

root = Path(__file__).resolve().parents[1]
source = Image.open(root / "assets/images/logo.png").convert("RGBA")
resources = root / "android/app/src/main/res"
for density, size in {"mdpi": 48, "hdpi": 72, "xhdpi": 96, "xxhdpi": 144, "xxxhdpi": 192}.items():
    folder = resources / f"mipmap-{density}"
    folder.mkdir(parents=True, exist_ok=True)
    source.resize((size, size), Image.Resampling.LANCZOS).save(folder / "ic_launcher.png")
# Extra safe margin for round, squircle, and other Android adaptive masks.
foreground = Image.new("RGBA", (432, 432))
foreground.alpha_composite(source.resize((300, 300), Image.Resampling.LANCZOS), (66, 66))
folder = resources / "drawable-nodpi"
folder.mkdir(parents=True, exist_ok=True)
foreground.save(folder / "ic_launcher_foreground.png")
folder = resources / "mipmap-anydpi-v26"
folder.mkdir(parents=True, exist_ok=True)
(folder / "ic_launcher.xml").write_text('''<?xml version="1.0" encoding="utf-8"?>
<adaptive-icon xmlns:android="http://schemas.android.com/apk/res/android">
    <background android:drawable="@color/launcher_background" />
    <foreground android:drawable="@drawable/ic_launcher_foreground" />
</adaptive-icon>
''', encoding="utf-8")
(resources / "values/brand_colors.xml").write_text('''<?xml version="1.0" encoding="utf-8"?>
<resources><color name="launcher_background">#163879</color></resources>
''', encoding="utf-8")
