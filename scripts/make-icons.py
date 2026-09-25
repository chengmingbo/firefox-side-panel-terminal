#!/usr/bin/env python3
"""Regenerate the extension's icons."""
from PIL import Image, ImageDraw, ImageFont
from pathlib import Path

OUT = Path(__file__).resolve().parent.parent / "src" / "icons"
OUT.mkdir(parents=True, exist_ok=True)

BG = (60, 60, 60, 255)
FG = (255, 106, 0, 255)
DIM = (220, 220, 220, 255)


def pick_font(size: int):
    paths = [
        "/System/Library/Fonts/Helvetica.ttc",
        "/System/Library/Fonts/SFNS.ttf",
        "/Library/Fonts/Arial.ttf",
        "/System/Library/Fonts/SFNSMono.ttf",
    ]
    for p in paths:
        try:
            return ImageFont.truetype(p, size)
        except Exception:
            continue
    return ImageFont.load_default()


def draw_icon(size: int) -> Image.Image:
    img = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    d = ImageDraw.Draw(img)
    r = max(2, int(size * 0.22))
    pad = max(1, size // 16)
    d.rounded_rectangle(
        (pad, pad, size - 1 - pad, size - 1 - pad),
        radius=r,
        fill=BG,
    )
    font = pick_font(int(size * 0.5))
    # ">" in orange, "_" in white, side by side.
    left = size * 0.22
    top = int(size * 0.25)
    d.text((left, top), ">", fill=FG, font=font)
    bbox = d.textbbox((left, top), ">", font=font)
    after_x = bbox[2] + max(2, size // 24)
    d.text((after_x, top), "_", fill=DIM, font=font)
    return img


def main() -> None:
    for s in (16, 32, 48, 96, 128):
        path = OUT / f"icon-{s}.png"
        draw_icon(s).save(path, "PNG")
        print(f"wrote {path}")


if __name__ == "__main__":
    main()
