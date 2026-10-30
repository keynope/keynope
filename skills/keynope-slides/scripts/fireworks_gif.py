#!/usr/bin/env python3
"""Render a seamless looping fireworks GIF with a transparent background, in chunky pixels.

Keynope's `fireworks` effect has no settings (about 20 small rockets per ~17 s loop).
For more and bigger bursts, import this GIF on the slide; it goes behind all content.
Needs Pillow:  python3 -m venv <scratch>/venv && <scratch>/venv/bin/pip install pillow
  <scratch>/venv/bin/python -I fireworks_gif.py out.gif [options]
--size WxH        pixels (default 640x354: a third of 1920x1062, about 55 rows tall)
--seconds S       loop length (default 10)    --delay MS   per frame (default 80)
--rockets N       rockets per loop (default 36, salvos included)
--salvo-every S   a 3-rocket salvo every S seconds (default 2.5, 0 = none)
--burst MIN,MAX   burst radius in slide pixels (default 170,330; the built-in effect is ~110)
--colours LIST    comma-separated hex colours (default a bright retro palette)
--avoid C0,R0,C1,R1  grid area (cols/rows) where most bursts are moved elsewhere, e.g. a title
--seed N          --sheet PNG  writes a 9-frame contact sheet over --bg (default #000000)
Keynope refuses animations over 32 megapixels summed across frames (W x H x frames),
so the script stops before writing one. It prints the geometry that places the GIF.
"""
import argparse
import math
import random
import sys

from PIL import Image, ImageDraw

COL, ROW = 1920 / 245, 1080 / 56
RETRO = "#ff5555,#ffff55,#55ffff,#ff55ff,#55ff55,#ffaa33,#7fdfff,#ffffff"
DRAG = 0.86


def hx(c):
    return tuple(int(c[i:i + 2], 16) for i in (1, 3, 5))


def main():
    ap = argparse.ArgumentParser(usage=__doc__)
    ap.add_argument("out")
    ap.add_argument("--size", default="640x354")
    ap.add_argument("--seconds", type=float, default=10)
    ap.add_argument("--delay", type=int, default=80)
    ap.add_argument("--rockets", type=int, default=36)
    ap.add_argument("--salvo-every", type=float, default=2.5)
    ap.add_argument("--burst", default="170,330")
    ap.add_argument("--colours", default=RETRO)
    ap.add_argument("--avoid")
    ap.add_argument("--seed", type=int, default=1)
    ap.add_argument("--sheet")
    ap.add_argument("--bg", default="#000000")
    a = ap.parse_args()

    W, H = (int(v) for v in a.size.lower().split("x"))
    T = round(a.seconds * 1000 / a.delay)
    mp = W * H * T / 1e6
    if mp > 32:
        sys.exit(f"{W}x{H} x {T} frames = {mp:.1f} megapixels; Keynope's limit is 32. Use a smaller --size or fewer frames.")
    S = W / 1920
    grid = max(2, round(8 * S))
    rmin, rmax = (float(v) for v in a.burst.split(","))
    bases = [c.strip() for c in a.colours.split(",")]
    rng = random.Random(a.seed)

    pal = [(0, 0, 0), (255, 255, 255), (255, 255, 160), (255, 170, 51)]  # transparent, flash, rocket head, rocket trail
    idx = {}
    for b in bases:
        for k in (1.0, 0.7, 0.45):
            idx[(b, k)] = len(pal)
            pal.append(tuple(int(v * k) for v in hx(b)))
    flat = [v for c in pal for v in c] + [0] * (768 - 3 * len(pal))

    avoid = None
    if a.avoid:
        c0, r0, c1, r1 = (float(v) for v in a.avoid.split(","))
        avoid = (c0 * COL * S, r0 * ROW * S, c1 * COL * S, r1 * ROW * S)

    def burst_pos():
        for _ in range(20):
            x, y = rng.uniform(90, 1830) * S, rng.uniform(70, 720) * S
            if not avoid or not (avoid[0] < x < avoid[2] and avoid[1] < y < avoid[3]) or rng.random() < 0.35:
                break
        return x, y

    salvos = [s * T / a.seconds for s in frange(0.5, a.seconds, a.salvo_every)] if a.salvo_every > 0 else []
    starts = sorted(rng.uniform(0, T) for _ in range(max(0, a.rockets - 3 * len(salvos))))
    for s in salvos:
        starts += [s, s + 2, s + 4]
    rockets = []
    for st in starts:
        bx, by = burst_pos()
        kind = rng.choices(["peony", "ring", "two", "willow"], [4, 2, 3, 1.5])[0]
        c1, c2 = rng.sample(bases, 2) if len(bases) > 1 else (bases[0], bases[0])
        if kind == "willow":
            c1 = c2 = "#ffaa33" if "#ffaa33" in bases else c1
        n = rng.randint(48, 84)
        parts = []
        for p in range(n):
            sp = 1.0 if kind == "ring" else (rng.uniform(0.9, 1.0) if p % 3 else rng.uniform(0.35, 0.9))
            parts.append((2 * math.pi * p / n + rng.uniform(-0.05, 0.05), sp, c2 if kind == "two" and p % 2 == 0 else c1, rng.random()))
        rockets.append(dict(start=st, x0=bx + rng.uniform(-60, 60) * S, bx=bx, by=by, rise=rng.randint(8, 11), kind=kind,
                            radius=rng.uniform(rmin, rmax) * S * (1.15 if kind == "willow" else 1),
                            life=rng.randint(24, 30) if kind == "willow" else rng.randint(15, 21), parts=parts))

    def particle(r, ang, sp, age):
        # Speed decays by DRAG each frame; gravity accumulates under the same drag.
        v0 = r["radius"] * sp * (1 - DRAG) / (1 - DRAG ** 16)
        k = (1 - DRAG ** age) / (1 - DRAG)
        g = (2.2 if r["kind"] == "willow" else 1.3) * S
        return r["bx"] + math.cos(ang) * v0 * k, r["by"] + math.sin(ang) * v0 * k + g * (age - k) / (1 - DRAG)

    def block(d, x, y, size, colour):
        gx, gy = int(x // grid) * grid, int(y // grid) * grid
        d.rectangle([gx, gy, gx + size - 1, gy + size - 1], fill=colour)

    def climb(r, age):
        e = 1 - (1 - max(0, age) / r["rise"]) ** 2
        return r["x0"] + (r["bx"] - r["x0"]) * e, H + 10 * S + (r["by"] - H - 10 * S) * e

    frames = []
    for f in range(T):
        im = Image.new("P", (W, H), 0)
        im.putpalette(flat)
        d = ImageDraw.Draw(im)
        for r in rockets:
            age = (f - r["start"]) % T  # wraps, so the loop is seamless
            if age < r["rise"]:
                for back in (3, 2, 1):
                    block(d, *climb(r, age - back * 0.6), grid // 2, 3)
                block(d, *climb(r, age), grid, 2)
                continue
            a_ = age - r["rise"]
            if a_ > r["life"]:
                continue
            fr = a_ / r["life"]
            if a_ == 0:
                for dx in (-1, 0, 1):
                    for dy in (-1, 0, 1):
                        block(d, r["bx"] + dx * grid, r["by"] + dy * grid, grid, 1)
            for ang, sp, col, tw in r["parts"]:
                if fr > 0.78 and (tw + f * 0.37) % 1 < 0.45:
                    continue  # twinkle out
                if fr < 0.8:
                    for back, k in ((2, 0.45), (1, 0.7)):
                        if a_ - back > 0:
                            block(d, *particle(r, ang, sp, a_ - back), grid // 2 if back == 2 else grid, idx[(col, k)])
                shade = 1.0 if fr < 0.55 else 0.7 if fr < 0.8 else 0.45
                block(d, *particle(r, ang, sp, a_), grid, 1 if fr < 0.1 else idx[(col, shade)])
        frames.append(im)

    frames[0].save(a.out, save_all=True, append_images=frames[1:], duration=a.delay, loop=0, transparency=0, disposal=2)
    if a.sheet:
        sheet = Image.new("RGB", (W * 3, H * 3), a.bg)
        for i in range(9):
            fr_ = frames[(i * T) // 9 + T // 18].convert("RGBA")
            bg = Image.new("RGBA", (W, H), a.bg)
            bg.alpha_composite(fr_)
            sheet.paste(bg.convert("RGB"), ((i % 3) * W, (i // 3) * H))
        sheet.save(a.sheet)
    h = round(1920 * H / W / ROW, 2)
    print(f"{a.out}: {W}x{H}, {T} frames ({mp:.1f} MP), {len(rockets)} rockets per {a.seconds:g} s loop")
    if h > 55.9:
        print(f"It is {h} rows tall at full width; at y 0 it can take the navigator title. Use a height <= {int(W * 55.9 * ROW / 1920)} px.")
    else:
        print('geometry: {"x":0,"y":%s,"width":245,"height":%s} with geometryResize, then image.alpha 100' % (round(56 - h - 0.02, 2), h))


def frange(start, stop, step):
    while start < stop:
        yield start
        start += step


if __name__ == "__main__":
    main()
