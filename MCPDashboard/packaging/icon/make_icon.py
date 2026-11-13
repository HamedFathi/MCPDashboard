"""Renders the MCPDashboard icon set from code.

Run from the repository's MCPDashboard folder:  python packaging/icon/make_icon.py
Requires Pillow. Writes icon.png (1024 px), MCPDashboard.ico and MCPDashboard.icns
into packaging/icon/.

Design: a Microsoft-blue tile holding a rising bar chart (the dashboard) whose
bar tops are linked as a connected graph (MCP servers wired to a client), ending
in a four-point sparkle (AI).
"""
import math
from pathlib import Path

from PIL import Image, ImageChops, ImageDraw, ImageFilter

OUT = Path(__file__).resolve().parent
S = 4  # supersampling factor
N = 1024 * S

TOP = (0x2B, 0x88, 0xD8)  # lighter Microsoft blue
BOTTOM = (0x00, 0x4E, 0x8C)  # deep Microsoft blue
WHITE = (255, 255, 255)
ACCENT = (0x9F, 0xE3, 0xFF)  # cyan glow for the AI sparkle


def p(v):
    return int(v * S)


def tile_mask(margin, radius):
    mask = Image.new("L", (N, N), 0)
    ImageDraw.Draw(mask).rounded_rectangle(
        (p(margin), p(margin), N - p(margin), N - p(margin)), p(radius), fill=255
    )
    return mask


def gradient():
    # diagonal gradient, top-left light to bottom-right deep
    v = Image.linear_gradient("L").resize((N, N))
    g = ImageChops.add(v.point(lambda x: x // 2), v.transpose(Image.ROTATE_90).point(lambda x: x // 2))
    top = Image.new("RGB", (N, N), TOP)
    bottom = Image.new("RGB", (N, N), BOTTOM)
    return Image.composite(bottom, top, g)


def sparkle(draw, cx, cy, r, fill):
    # four-point star with curved concave sides (a softened astroid)
    pts = []
    for i in range(360):
        t = math.radians(i)
        c, s = math.cos(t), math.sin(t)
        pts.append((cx + r * math.copysign(abs(c) ** 2.6, c), cy + r * math.copysign(abs(s) ** 2.6, s)))
    draw.polygon(pts, fill=fill)


def render(detailed=True):
    margin, radius = 56, 210
    base = Image.new("RGBA", (N, N), (0, 0, 0, 0))
    base.paste(gradient(), mask=tile_mask(margin, radius))

    # soft top highlight for depth
    hl = Image.new("L", (N, N), 0)
    ImageDraw.Draw(hl).ellipse((p(-200), p(-620), p(1224), p(470)), fill=46)
    hl = ImageChops.multiply(hl.filter(ImageFilter.GaussianBlur(p(40))), tile_mask(margin, radius))
    base = Image.composite(Image.new("RGBA", (N, N), WHITE + (255,)), base, hl)

    layer = Image.new("RGBA", (N, N), (0, 0, 0, 0))
    d = ImageDraw.Draw(layer)

    baseline = 790
    bar_w = 132
    xs = [262, 452, 642]  # bar centers
    tops = [600, 470, 540]
    if detailed:
        for x, t in zip(xs, tops):
            d.rounded_rectangle(
                (p(x - bar_w / 2), p(t), p(x + bar_w / 2), p(baseline)), p(28), fill=WHITE + (80,)
            )
        # baseline axis
        d.rounded_rectangle((p(180), p(baseline + 26), p(844), p(baseline + 50)), p(12), fill=WHITE + (150,))

    # connected graph across bar tops, ending at the AI sparkle
    nodes = list(zip(xs, tops)) + [(760, 310)]
    line_w = 34 if detailed else 56
    for a, b in zip(nodes, nodes[1:]):
        d.line((p(a[0]), p(a[1]), p(b[0]), p(b[1])), fill=WHITE + (255,), width=p(line_w))
    node_r = 52 if detailed else 74
    for x, y in nodes[:-1]:
        d.ellipse((p(x - node_r), p(y - node_r), p(x + node_r), p(y + node_r)), fill=WHITE + (255,))
        if detailed:
            inner = node_r - 22
            d.ellipse((p(x - inner), p(y - inner), p(x + inner), p(y + inner)), fill=BOTTOM + (255,))

    # AI sparkle with glow
    sx, sy = nodes[-1]
    glow = Image.new("RGBA", (N, N), (0, 0, 0, 0))
    sparkle(ImageDraw.Draw(glow), p(sx), p(sy), p(170), ACCENT + (140,))
    glow = glow.filter(ImageFilter.GaussianBlur(p(36)))
    layer = Image.alpha_composite(glow, layer)
    d = ImageDraw.Draw(layer)
    sparkle(d, p(sx), p(sy), p(140 if detailed else 170), WHITE + (255,))
    if detailed:
        sparkle(d, p(sx - 140), p(sy - 110), p(48), WHITE + (230,))

    # keep the glow inside the tile
    alpha = ImageChops.multiply(layer.getchannel("A"), tile_mask(margin, radius))
    layer.putalpha(alpha)
    img = Image.alpha_composite(base, layer)
    return img.resize((1024, 1024), Image.LANCZOS)


def main():
    full = render(detailed=True)
    small = render(detailed=False)  # bolder glyph for 16-32 px
    full.save(OUT / "icon.png")

    sizes = [16, 20, 24, 32, 40, 48, 64, 128, 256]
    frames = [(small if s <= 32 else full).resize((s, s), Image.LANCZOS) for s in sizes]
    frames[-1].save(OUT / "MCPDashboard.ico", sizes=[(s, s) for s in sizes], append_images=frames[:-1])
    full.save(OUT / "MCPDashboard.icns")
    print("wrote", *(f.name for f in sorted(OUT.glob("*")) if f.suffix != ".py"))


if __name__ == "__main__":
    main()
