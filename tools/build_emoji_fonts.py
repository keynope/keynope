#!/usr/bin/env python3
"""Build every canonical emoji as a lossless COLRv0 TrueType/WOFF2 face.

Run with fonttools[woff]. Per-artwork faces avoid COLRv0's 16-bit palette,
layer and glyph limits and allow exports to include only the artwork used.
No system emoji fonts or original Noto artwork are required.
"""
import argparse
import collections
import concurrent.futures
import gzip
import io
import json
from pathlib import Path
import struct
import zipfile

from fontTools.fontBuilder import FontBuilder
from fontTools.pens.ttGlyphPen import TTGlyphPen
from fontTools.colorLib.builder import buildCOLR, buildCPAL
from fontTools.feaLib.builder import addOpenTypeFeaturesFromString

ROOT = Path(__file__).resolve().parents[1]


def artworks():
    data = gzip.decompress((ROOT / 'assets/emoji/keynope-emoji-glyphs.bin.gz').read_bytes())
    assert data[:5] == b'KNEG1'
    width, height, count = struct.unpack_from('>HHI', data, 5)
    assert (width, height) == (80, 40)
    pos = 13
    for _ in range(count):
        length, = struct.unpack_from('>H', data, pos)
        pos += 2
        key = data[pos:pos + length].decode('ascii')
        pos += length
        cells = data[pos:pos + width * height * 4]
        pos += len(cells)
        yield key, cells
    assert pos == len(data)


def build(item):
    key, cells = item
    # Each terminal cell has two by two quadrants; its optical height is
    # twice its width. Normalize the 160x80 subpixel grid to a square em.
    colors = collections.defaultdict(list)
    for y in range(80):
        x = 0
        while x < 160:
            def pixel(xx):
                at = ((y // 2) * 80 + xx // 2) * 4
                return tuple(cells[at + 1:at + 4]) if cells[at] & (1 << ((y % 2) * 2 + xx % 2)) else None
            color = pixel(x)
            end = x + 1
            while end < 160 and pixel(end) == color:
                end += 1
            if color is not None:
                colors[color].append((x * 10, (79 - y) * 20, end * 10, (80 - y) * 20))
            x = end
    codepoints = [int(part, 16) for part in key.split('_')]
    components = {point: 'part' + format(point, 'x') for point in codepoints} if len(codepoints) > 1 else {}
    names = ['.notdef', 'emoji'] + list(components.values()) + ['layer' + str(i) for i in range(len(colors))]
    glyphs = {name: TTGlyphPen(None).glyph() for name in ['.notdef', 'emoji', *components.values()]}
    layers = []
    palette = []
    for i, (color, rectangles) in enumerate(sorted(colors.items())):
        pen = TTGlyphPen(None)
        for left, bottom, right, top in rectangles:
            pen.moveTo((left, bottom)); pen.lineTo((left, top))
            pen.lineTo((right, top)); pen.lineTo((right, bottom)); pen.closePath()
        name = 'layer' + str(i)
        glyphs[name] = pen.glyph()
        layers.append((name, i))
        palette.append(tuple(channel / 255 for channel in color) + (1.0,))
    fb = FontBuilder(1600, isTTF=True)
    fb.setupGlyphOrder(names)
    # Preserve the canvas renderer's PUA mapping. Native text shaping uses the
    # actual Unicode sequence, keeping authored text and DOM offsets intact.
    cmap = {0xE000: 'emoji'}
    if components:
        cmap.update(components)
    else:
        cmap[codepoints[0]] = 'emoji'
    fb.setupCharacterMap(cmap, uvs=[(point, 0xFE0F, None) for point in set(codepoints)])
    fb.setupGlyf(glyphs)
    # Layer glyphs must retain their own left bearing. Setting every bearing to
    # zero moves each colour's contours independently and scrambles the artwork.
    fb.setupHorizontalMetrics({name: (1600, getattr(glyphs[name], 'xMin', 0)) for name in names})
    fb.setupHorizontalHeader(ascent=1600, descent=0, lineGap=0)
    fb.setupNameTable({'familyName': 'Keynope Emoji ' + key, 'styleName': 'Regular',
                      'uniqueFontIdentifier': 'KeynopeEmoji-' + key,
                      'fullName': 'Keynope Emoji ' + key, 'psName': 'KeynopeEmoji-' + key,
                      'version': 'Version 1.000',
                      'copyright': 'Derived from Noto Emoji. Copyright 2013 Google LLC.',
                      'licenseDescription': 'SIL Open Font License 1.1. See bundled OFL.txt and NOTICE.txt.',
                      'licenseInfoURL': 'https://openfontlicense.org/'})
    fb.setupOS2(sTypoAscender=1600, sTypoDescender=0, sTypoLineGap=0, usWinAscent=1600, usWinDescent=0)
    fb.setupPost(); fb.setupMaxp()
    if components:
        sequence = ' '.join(components[point] for point in codepoints)
        # Required ligatures must survive disabled discretionary ligatures.
        # Default-ignorable variation selectors are ignored by shaping; the
        # asset keys deliberately omit FE0F, matching emojiAssetKey in Go.
        addOpenTypeFeaturesFromString(fb.font, 'languagesystem DFLT dflt; feature rlig { sub ' + sequence + ' by emoji; } rlig;')
    fb.font['COLR'] = buildCOLR({'emoji': layers}, version=0)
    fb.font['CPAL'] = buildCPAL([palette])
    fb.font['head'].created = fb.font['head'].modified = 3849984000
    raw = io.BytesIO(); fb.font.save(raw)
    fb.font.flavor = 'woff2'
    compressed = io.BytesIO(); fb.font.save(compressed)
    return key, raw.getvalue(), compressed.getvalue(), len(colors)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--jobs', type=int, default=4)
    parser.add_argument('--ttf-dir', type=Path, default=ROOT / 'output/emoji-fonts')
    args = parser.parse_args()
    args.ttf_dir.mkdir(parents=True, exist_ok=True)
    items = sorted(artworks())
    destination = ROOT / 'assets/emoji/keynope-emoji-fonts.zip'
    manifest = {}
    temporary = destination.with_suffix('.zip.tmp')
    with zipfile.ZipFile(temporary, 'w', compression=zipfile.ZIP_STORED) as archive:
        with concurrent.futures.ProcessPoolExecutor(max_workers=args.jobs) as pool:
            for index, (key, ttf, woff, colors) in enumerate(pool.map(build, items), 1):
                (args.ttf_dir / (key + '.ttf')).write_bytes(ttf)
                entry = zipfile.ZipInfo(key + '.woff2', date_time=(2026, 1, 1, 0, 0, 0))
                archive.writestr(entry, woff)
                manifest[key] = {'bytes': len(woff), 'colors': colors}
                if index % 100 == 0:
                    print(f'{index}/{len(items)} converted', flush=True)
        archive.writestr(zipfile.ZipInfo('manifest.json', date_time=(2026, 1, 1, 0, 0, 0)), json.dumps(manifest, sort_keys=True))
    temporary.replace(destination)
    print(f'Converted all {len(items)} artworks: {destination.stat().st_size:,} bytes in {destination}')


if __name__ == '__main__':
    main()
