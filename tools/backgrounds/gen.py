#!/usr/bin/env python3
"""Procedural wallpapers for HomeView. Everything here is original work made
from math: no third-party artwork. Run: python3 gen.py OUTDIR"""
import sys, os
import numpy as np
from PIL import Image, ImageDraw, ImageFilter

W, H = 1920, 1080
OUT = sys.argv[1] if len(sys.argv) > 1 else "out"
os.makedirs(OUT, exist_ok=True)

def hexc(h): return np.array([int(h[i:i+2], 16) for i in (1, 3, 5)], float) / 255
def lerp(a, b, t): return a + (b - a) * t
def smooth(x): x = np.clip(x, 0, 1); return x * x * (3 - 2 * x)

def vgrad(stops, w=W, h=H):
    t = np.linspace(0, 1, h)
    pos = [p for p, _ in stops]
    cols = np.array([hexc(c) for _, c in stops])
    ch = [np.interp(t, pos, cols[:, i]) for i in range(3)]
    return np.repeat(np.stack(ch, -1)[:, None, :], w, axis=1)

def fbm(w, h, octaves=5, base=3, seed=0, pers=0.5):
    r = np.random.default_rng(seed)
    tot = np.zeros((h, w), np.float32); amp, s = 1.0, 0.0
    for o in range(octaves):
        n = base * 2 ** o
        small = r.random((max(2, int(n * h / w)) + 1, n + 1)).astype(np.float32)
        tot += np.clip(np.asarray(Image.fromarray(small).resize((w, h), Image.BICUBIC)), 0, 1) * amp
        s += amp; amp *= pers
    return tot / s

def noise1d(n, seed, octaves=5, base=3, pers=0.5):
    r = np.random.default_rng(seed); x = np.linspace(0, 1, n)
    tot = np.zeros(n); amp, s = 1.0, 0.0
    for o in range(octaves):
        k = base * 2 ** o
        pts = r.random(k + 1)
        # cosine interpolation between random points
        i = np.minimum((x * k).astype(int), k - 1); f = x * k - i
        f = (1 - np.cos(f * np.pi)) / 2
        tot += (pts[i] * (1 - f) + pts[i + 1] * f) * amp
        s += amp; amp *= pers
    return tot / s

def save(name, arr, grain=0.0035):
    r = np.random.default_rng(7)
    a = np.clip(arr + r.normal(0, grain, arr.shape), 0, 1)  # dither: hides banding
    im = Image.fromarray((a * 255 + 0.5).astype(np.uint8))
    im.save(f"{OUT}/{name}.webp", quality=80, method=6)
    im.resize((480, 270), Image.LANCZOS).save(f"{OUT}/{name}-thumb.webp", quality=78, method=6)
    print(name, os.path.getsize(f"{OUT}/{name}.webp") // 1024, "KB")

Y = np.linspace(0, 1, H)[:, None]; X = np.linspace(0, 1, W)[None, :]
YY, XX = np.mgrid[0:H, 0:W].astype(np.float32)

def layer(img, ridge, top, bottom, depth=1.0, haze=None, hazeamt=0.0):
    """Fill everything below a ridge line with a vertical colour ramp."""
    ry = ridge[None, :] * H
    mask = smooth((YY - ry) / 1.6 + 0.5)[..., None]
    d = np.clip((YY - ry) / (H * 0.45 * depth), 0, 1)[..., None]
    col = lerp(hexc(top), hexc(bottom), d)
    if haze is not None: col = lerp(col, hexc(haze), hazeamt)
    return img * (1 - mask) + col * mask

# 1 ---------------------------------------------------------------- aurora
def aurora():
    img = vgrad([(0, "#040814"), (0.6, "#0a1530"), (1, "#0d2036")])
    glow = np.zeros((H, W, 3))
    rng = np.random.default_rng(11)
    cols = [hexc("#34d399"), hexc("#22d3ee"), hexc("#a78bfa")]
    for k in range(4):
        yc = 0.28 + 0.07 * k + 0.12 * (noise1d(W, 20 + k, 3, 2) - 0.5)
        width = 0.05 + 0.035 * rng.random()
        band = np.exp(-(((Y - yc[None, :]) / width) ** 2))
        rays = 0.35 + 0.65 * noise1d(W, 40 + k, 5, 10)[None, :]
        t = np.clip(X * 1.2 + k * 0.12 - 0.1, 0, 1)
        c = lerp(cols[0], cols[1], smooth(t * 2))[..., :] if False else None
        mix = np.stack([lerp(lerp(cols[0][i], cols[1][i], smooth(t * 2)), cols[2][i], smooth(t * 2 - 1)) for i in range(3)], -1)
        glow += band[..., None] * rays[..., None] * mix * (0.55 - 0.08 * k)
    g = Image.fromarray((np.clip(glow, 0, 1) * 255).astype(np.uint8)).filter(ImageFilter.GaussianBlur(14))
    img = 1 - (1 - img) * (1 - np.asarray(g) / 255 * 0.95)
    # stars
    st = np.zeros((H, W)); n = 650
    px, py = rng.integers(0, W, n), rng.integers(0, int(H * 0.8), n)
    st[py, px] = rng.random(n) ** 3
    sg = np.asarray(Image.fromarray((st * 255).astype(np.uint8)).filter(ImageFilter.GaussianBlur(0.8))) / 255
    img = np.clip(img + (sg * 2.2)[..., None] * 0.9, 0, 1)
    # dark ridge at the bottom
    img = layer(img, 0.9 + 0.05 * noise1d(W, 5, 4, 3), "#050a14", "#02050b")
    return img
save("aurora", aurora())

# 2 ----------------------------------------------------------------- dusk
def dusk():
    img = vgrad([(0, "#1b1650"), (0.35, "#5b21b6"), (0.55, "#be185d"), (0.72, "#fb923c"), (0.78, "#fde68a"), (1, "#1e1040")])
    cx, cy = 0.62 * W, 0.74 * H
    r = np.hypot(XX - cx, (YY - cy) * 1.15)
    sun = np.exp(-(r / 520) ** 2) * 0.55 + np.exp(-(r / 90) ** 2) * 0.9
    img = np.clip(img + sun[..., None] * hexc("#ffd29a"), 0, 1)
    for i, (base, amp, a, b) in enumerate([(0.80, 0.05, "#7c3aed", "#4c1d95"), (0.86, 0.06, "#4c1d95", "#2e1065"), (0.93, 0.05, "#2e1065", "#150a33")]):
        ridge = base + amp * (noise1d(W, 60 + i, 5, 2 + i) - 0.5)
        img = layer(img, ridge, a, b, haze="#f59e6b", hazeamt=0.30 - 0.1 * i)
    return img
save("crepusculo", dusk())

# 3 ----------------------------------------------------------------- ocean
def ocean():
    img = vgrad([(0, "#0b3a63"), (0.5, "#0a5a8a"), (1, "#052a49")])
    for i in range(9):
        base = 0.25 + i * 0.075
        ridge = base + 0.045 * np.sin(2 * np.pi * (X[0] * (1.1 + 0.2 * i) + 0.13 * i)) + 0.03 * (noise1d(W, 90 + i, 3, 2) - 0.5)
        mask = smooth((YY - ridge[None, :] * H) / 2.0 + 0.5)[..., None]
        col = lerp(hexc("#38bdf8"), hexc("#0e7490"), i / 8)
        img = img * (1 - mask * (0.10 + 0.03 * i)) + col * mask * (0.10 + 0.03 * i)
        edge = np.exp(-(((YY - ridge[None, :] * H) / 2.2) ** 2))[..., None]
        img = np.clip(img + edge * hexc("#bae6fd") * 0.10, 0, 1)
    return img
save("oceano", ocean())

# 4 -------------------------------------------------------------- mountains
def mountains():
    img = vgrad([(0, "#9ec5e8"), (0.45, "#f2d7c4"), (0.7, "#fbe8d6"), (1, "#fbe8d6")])
    cx, cy = 0.3 * W, 0.42 * H
    r = np.hypot(XX - cx, YY - cy)
    img = np.clip(img + (np.exp(-(r / 380) ** 2) * 0.35)[..., None] * hexc("#fff3dc"), 0, 1)
    spec = [("#a9b9d6", "#8c9fc4", 0.50), ("#8da2c9", "#7288b3", 0.58), ("#7189b4", "#576e9c", 0.67),
            ("#566f9b", "#3f567f", 0.76), ("#3d5479", "#27395a", 0.86)]
    for i, (a, b, base) in enumerate(spec):
        ridge = base + 0.16 * (noise1d(W, 120 + i, 6, 2 + i, 0.55) - 0.5)
        img = layer(img, ridge, a, b, depth=0.8, haze="#fbe8d6", hazeamt=0.55 - 0.12 * i)
    return img
save("montanhas", mountains())

# 5 ----------------------------------------------------------------- mesh
def mesh():
    warp = (fbm(W, H, 3, 2, 3) - 0.5) * 0.35
    px, py = X + warp, Y + warp * 0.6
    blobs = [("#fda4af", 0.12, 0.2, 0.40), ("#c4b5fd", 0.55, 0.1, 0.38), ("#93c5fd", 0.9, 0.3, 0.42),
             ("#a7f3d0", 0.2, 0.9, 0.40), ("#fde68a", 0.7, 0.85, 0.40), ("#f9a8d4", 0.45, 0.5, 0.30)]
    num = np.zeros((H, W, 3)); den = np.zeros((H, W, 1))
    for c, bx, by, s in blobs:
        d2 = ((px - bx) * W / H) ** 2 + (py - by) ** 2
        w = np.exp(-d2 / (s * s))[..., None]
        num += w * hexc(c); den += w
    return np.clip(num / den, 0, 1)
save("malha-pastel", mesh())

# 6 ------------------------------------------------------------- starfield
def starfield():
    img = vgrad([(0, "#05060f"), (1, "#0a0a1c")])
    n1 = np.clip((fbm(W, H, 6, 2, 31) - 0.42) * 3.2, 0, 1) ** 1.6
    n2 = np.clip((fbm(W, H, 5, 3, 77) - 0.50) * 3.0, 0, 1) ** 1.8
    img = np.clip(img + n1[..., None] * hexc("#6d28d9") * 0.50 + n2[..., None] * hexc("#2563eb") * 0.42, 0, 1)
    rng = np.random.default_rng(5)
    st = np.zeros((H, W)); n = 1400
    px, py = rng.integers(0, W, n), rng.integers(0, H, n)
    st[py, px] = rng.pareto(3.0, n).clip(0, 1) * 0.9 + 0.1
    sg = np.asarray(Image.fromarray((st * 255).astype(np.uint8)).filter(ImageFilter.GaussianBlur(0.7))) / 255
    big = np.zeros((H, W)); m = 26
    bx, by = rng.integers(40, W - 40, m), rng.integers(40, H - 40, m)
    big[by, bx] = 1
    bg = np.asarray(Image.fromarray((big * 255).astype(np.uint8)).filter(ImageFilter.GaussianBlur(5))) / 255
    return np.clip(img + (sg * 2.4)[..., None] + (bg * 9)[..., None] * hexc("#c7d2fe") * 0.9, 0, 1)
save("estrelas", starfield())

# 7 --------------------------------------------------------- topographic
def topo():
    img = vgrad([(0, "#0f172a"), (1, "#0b1220")])
    f = fbm(W, H, 5, 3, 3, 0.5) * 22
    fr = f - np.floor(f)
    line = 1 - np.clip(np.minimum(fr, 1 - fr) / 0.045, 0, 1)
    major = (np.floor(f + 0.5) % 5 == 0).astype(float)
    a = (line * (0.20 + 0.28 * major))[..., None]
    img = lerp(img, hexc("#5eead4"), a)
    r = np.hypot((XX - W * 0.5) / W, (YY - H * 0.5) / H)
    return np.clip(img * (1.10 - 0.55 * r[..., None]), 0, 1)
save("topografico", topo())

# 8 --------------------------------------------------------------- lowpoly
def lowpoly():
    S = 2; w, h = W * S, H * S
    cols, rows = 30, 17
    rng = np.random.default_rng(21)
    xs = np.linspace(-0.05, 1.05, cols + 1); ys = np.linspace(-0.05, 1.05, rows + 1)
    pts = np.zeros((rows + 1, cols + 1, 2))
    for j in range(rows + 1):
        for i in range(cols + 1):
            jx = (rng.random() - .5) * 0.6 / cols; jy = (rng.random() - .5) * 0.6 / rows
            pts[j, i] = (xs[i] + jx, ys[j] + jy)
    pal = [(0, hexc("#1e1b4b")), (0.4, hexc("#4338ca")), (0.7, hexc("#0ea5e9")), (1, hexc("#5eead4"))]
    def colour(x, y):
        t = np.clip(0.55 * x + 0.45 * (1 - y) + (rng.random() - .5) * 0.10, 0, 1)
        for (p0, c0), (p1, c1) in zip(pal, pal[1:]):
            if t <= p1: return lerp(c0, c1, (t - p0) / (p1 - p0))
        return pal[-1][1]
    im = Image.new("RGB", (w, h)); d = ImageDraw.Draw(im)
    for j in range(rows):
        for i in range(cols):
            a, b, c, e = pts[j, i], pts[j, i + 1], pts[j + 1, i + 1], pts[j + 1, i]
            tris = [(a, b, c), (a, c, e)] if rng.random() < .5 else [(a, b, e), (b, c, e)]
            for t in tris:
                cx = sum(p[0] for p in t) / 3; cy = sum(p[1] for p in t) / 3
                col = colour(cx, cy)
                d.polygon([(p[0] * w, p[1] * h) for p in t], fill=tuple(int(v * 255) for v in col))
    im = im.resize((W, H), Image.LANCZOS)
    a = np.asarray(im).astype(np.float32) / 255
    r = np.hypot((XX - W * 0.5) / W, (YY - H * 0.5) / H)
    return np.clip(a * (1.08 - 0.35 * r[..., None]), 0, 1)
save("poligonos", lowpoly())

# 9 ------------------------------------------------------------------ forest
def forest():
    S = 2; w, h = W * S, H * S
    sky = vgrad([(0, "#bfe3d6"), (0.5, "#f6efc9"), (1, "#f6efc9")], w, h)
    base = Image.fromarray((sky * 255).astype(np.uint8)).convert("RGBA")
    rng = np.random.default_rng(3)
    fog = hexc("#f3f0cf")
    layers = [("#8fb8a3", 0.52, 26, 0.95), ("#6fa08c", 0.60, 34, 0.85), ("#4f8573", 0.69, 46, 0.70),
              ("#356a5a", 0.79, 62, 0.50), ("#1f4a40", 0.91, 84, 0.20)]
    for i, (col, yb, size, fogamt) in enumerate(layers):
        lay = Image.new("RGBA", (w, h), (0, 0, 0, 0)); d = ImageDraw.Draw(lay)
        ridge = yb + 0.035 * (noise1d(w, 200 + i, 4, 2 + i) - .5) * 2
        d.polygon([(0, h)] + [(x, ridge[x] * h) for x in range(0, w, 8)] + [(w - 1, ridge[-1] * h), (w, h)], fill=col)
        x = 0
        while x < w:
            th = size * S * (0.7 + 0.9 * rng.random()); tw = th * 0.36
            yb_ = ridge[min(x, w - 1)] * h + 4
            for k in range(4):
                top = yb_ - th * (1 - k * 0.2); bot = yb_ - th * (0.62 - k * 0.2) + th * 0.18
                half = tw * (0.45 + k * 0.2)
                d.polygon([(x, top), (x - half, bot), (x + half, bot)], fill=col)
            x += int(tw * (0.55 + 0.9 * rng.random()))
        arr = np.asarray(lay).astype(np.float32) / 255
        # fog: strongest at the top of each layer
        top_fade = np.clip(1 - (np.arange(h)[:, None] / h - (yb - 0.12)) / 0.35, 0, 1)[..., None]
        rgb = lerp(arr[..., :3], fog, np.clip(fogamt * top_fade, 0, 1))
        lay2 = np.concatenate([rgb, arr[..., 3:]], -1)
        base = Image.alpha_composite(base, Image.fromarray((lay2 * 255).astype(np.uint8), "RGBA"))
    out = base.convert("RGB").resize((W, H), Image.LANCZOS)
    return np.asarray(out).astype(np.float32) / 255
save("floresta", forest())

# 10 ------------------------------------------------------------------ dunes
def dunes():
    img = vgrad([(0, "#f7d9b6"), (0.5, "#fbe9d0"), (1, "#fbe9d0")])
    spec = [("#f1cf9f", "#e2b27b", 0.50), ("#e8bd88", "#d39e66", 0.60), ("#dca873", "#c48655", 0.70),
            ("#cd9460", "#a96a42", 0.81), ("#b57a4d", "#8a5133", 0.93)]
    for i, (a, b, base) in enumerate(spec):
        ridge = base + 0.07 * (noise1d(W, 300 + i, 3, 1 + i, 0.4) - .5) * 2 + 0.02 * np.sin(2 * np.pi * X[0] * (1 + i * .4) + i)
        img = layer(img, ridge, a, b, depth=0.55, haze="#fbe9d0", hazeamt=0.35 - 0.07 * i)
        # shaded slope on one side of each dune
        slope = np.gradient(ridge)
        sh = np.clip(-slope * 900, 0, 1)[None, :] * np.exp(-(((YY - ridge[None, :] * H)) / 160) ** 2)
        mask = smooth((YY - ridge[None, :] * H) / 1.6 + 0.5)
        img = lerp(img, hexc(b) * 0.82, (sh * mask * 0.55)[..., None])
    return img
save("dunas", dunes())

# 11 ---------------------------------------------------------------- graphite
def graphite():
    img = vgrad([(0, "#161b26"), (1, "#0a0d14")])
    r = np.hypot((XX - W * 0.7) / W, (YY - H * 0.1) / H)
    img = np.clip(img + (np.exp(-(r / 0.38) ** 2) * 0.09)[..., None] * hexc("#8aa4ff"), 0, 1)
    d = ((XX / W - 0.35) * 1.0 + (YY / H - 0.5) * 0.55)
    img = np.clip(img + (np.exp(-(d / 0.07) ** 2) * 0.035)[..., None], 0, 1)
    return img
save("grafite", graphite(), grain=0.006)
