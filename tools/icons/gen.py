#!/usr/bin/env python3
"""Generates the PWA icons (internal/web/static/pwa/*.png) from the same drawing as
favicon.svg. Needs Pillow:  python3 tools/icons/gen.py internal/web/static/pwa"""
import sys
from PIL import Image, ImageDraw

BLUE, WHITE = (37, 99, 235, 255), (255, 255, 255, 255)
SS = 4  # supersampling for smooth edges


def house(d, ox, oy, k, w):
    """Draws the favicon's house on a 24x24 grid scaled by k at (ox, oy)."""
    def p(x, y):
        return (ox + x * k, oy + y * k)

    def line(pts):
        pts = [p(*q) for q in pts]
        d.line(pts, fill=WHITE, width=int(w * k), joint="curve")
        r = w * k / 2
        for x, y in (pts[0], pts[-1]):
            d.ellipse((x - r, y - r, x + r, y + r), fill=WHITE)

    line([(3, 11.5), (12, 4), (21, 11.5)])
    line([(5.5, 10.5), (5.5, 20), (18.5, 20), (18.5, 10.5)])
    cx, cy = p(12, 14.5)
    r = 2.2 * k
    d.ellipse((cx - r, cy - r, cx + r, cy + r), fill=WHITE)


def make(size, scale, rounded, path):
    s = size * SS
    img = Image.new("RGBA", (s, s), (0, 0, 0, 0))
    d = ImageDraw.Draw(img)
    if rounded:
        d.rounded_rectangle((0, 0, s - 1, s - 1), radius=int(s * 5 / 24), fill=BLUE)
    else:
        d.rectangle((0, 0, s, s), fill=BLUE)
    k = s * scale / 24
    off = (s - 24 * k) / 2
    house(d, off, off, k, 2)
    img.resize((size, size), Image.LANCZOS).save(path, optimize=True)


out = sys.argv[1] if len(sys.argv) > 1 else "."
make(192, 1.0, True, f"{out}/icon-192.png")
make(512, 1.0, True, f"{out}/icon-512.png")
make(512, 0.6, False, f"{out}/icon-maskable-512.png")  # house inside the 80% safe zone
make(180, 0.8, False, f"{out}/apple-touch-icon.png")
