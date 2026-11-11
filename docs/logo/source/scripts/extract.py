"""Extract LeaSSH logos from the reference board (upscaled x4) as PNG."""
import os, sys, glob, shutil
import numpy as np, cv2
from PIL import Image, ImageDraw, ImageFont, ImageFilter
from scipy import ndimage

S = 4
SRC = sys.argv[1] if len(sys.argv) > 1 else "work/ref_x4.png"
OUT = "leassh-png"
for d in ("logos", "symboles", "icone", "favicon", "apercus"):
    os.makedirs(f"{OUT}/{d}", exist_ok=True)

BIG = np.array(Image.open(SRC).convert("RGB"))
FIG = glob.glob("fonts/fontsource-figtree-*/package/files/figtree-latin-400-normal.woff")[0]

TAG_FR = ("Serveur MCP", "Actions sécurisées", "pour agents IA")


def crop(x0, y0, x1, y1):
    return BIG[y0 * S:y1 * S, x0 * S:x1 * S].copy()


from rembg import remove, new_session
_SESSION = None


def object_mask(rgb):
    """Soft object mask from IS-Net (rembg), binarised and slightly eroded."""
    global _SESSION
    if _SESSION is None:
        _SESSION = new_session("isnet-general-use")
    im = Image.fromarray(rgb)
    k = 1024 / max(im.size)
    small = im.resize((round(im.width * k), round(im.height * k)), Image.LANCZOS) if k < 1 else im
    m = remove(small, session=_SESSION, only_mask=True).resize(im.size, Image.LANCZOS)
    m = np.array(m).astype(float) / 255
    m = np.clip((m - 0.35) / 0.3, 0, 1)
    m = ndimage.grey_erosion(m, size=(5, 5))
    return m


def transparent(rgb, bg, obj=None, floor=0.05, lighter=False):
    """Colour-to-alpha against bg (darker-than-bg pixels only), with noise floor;
    pixels inside the object mask stay opaque with their original colour."""
    c = rgb.astype(float)
    b = np.array(bg, float)
    a = np.zeros(c.shape[:2])
    for i in range(3):
        if lighter:
            a = np.maximum(a, np.clip((c[..., i] - b[i]) / max(255 - b[i], 1), 0, 1))
        else:
            a = np.maximum(a, np.clip((b[i] - c[..., i]) / max(b[i], 1), 0, 1))
    a = np.clip((a - floor) / (1 - floor), 0, 1)
    a = ndimage.median_filter(a, size=3)
    safe = np.maximum(a, 1e-3)
    un = np.clip((c - b) / safe[..., None] + b, 0, 255)
    un = np.where(a[..., None] > 1e-3, un, c)
    if obj is not None:
        un = un * (1 - obj[..., None]) + c * obj[..., None]
        a = np.maximum(a, obj)
    return Image.fromarray(np.dstack([un, a * 255]).astype(np.uint8), "RGBA")


def pad_right(rgb, n):
    edge = rgb[:, -6:-1].mean(1, keepdims=True).astype(np.uint8)
    return np.concatenate([rgb, np.repeat(edge, n, 1)], 1)


def with_mark_mask(rgb, mark_w):
    """Object mask restricted to the symbol part (left mark_w pixels)."""
    m = np.zeros(rgb.shape[:2])
    m[:, :mark_w] = object_mask(rgb[:, :mark_w])
    return m


def tag_width(cap):
    f = ImageFont.truetype(FIG, 10)
    capf = f.getbbox("H")[3] - f.getbbox("H")[1]
    f = ImageFont.truetype(FIG, round(10 * cap / capf))
    sp = f.getlength(" ")
    return f.getlength(TAG_FR[0]) + 2 * sp + 2 * cap * 0.18 + f.getlength(TAG_FR[1])


def trim(im, pad=12):
    """Crop to visible content plus pad (pad can extend beyond the image)."""
    a = np.array(im)[..., 3] if im.mode == "RGBA" else None
    if a is None:
        return im
    ys, xs = np.where(a > 8)
    box = (xs.min() - pad, ys.min() - pad, xs.max() + pad + 1, ys.max() + pad + 1)
    return im.crop(box)


def on(bgcol, im):
    base = Image.new("RGBA", im.size, bgcol)
    base.alpha_composite(im)
    return base.convert("RGB")


def erase(rgb, box):
    """Inpaint a box (in board coordinates relative to crop, x4) with surrounding background."""
    x0, y0, x1, y1 = box
    mask = np.zeros(rgb.shape[:2], np.uint8)
    mask[y0:y1, x0:x1] = 255
    return cv2.inpaint(rgb, mask, 6, cv2.INPAINT_TELEA)


def draw_tag(img, x, base1, base2, cap, color, lines=TAG_FR):
    """Draw the two-line baseline with Figtree and a round bullet like the board."""
    d = ImageDraw.Draw(img)
    f = ImageFont.truetype(FIG, 10)
    capf = f.getbbox("H")[3] - f.getbbox("H")[1]
    size = round(10 * cap / capf)
    f = ImageFont.truetype(FIG, size)
    def put(t, x, base):
        d.text((x, base), t, font=f, fill=color, anchor="ls")
        return x + f.getlength(t)
    xx = put(lines[0], x, base1)
    sp = f.getlength(" ")
    r = cap * 0.18
    cx = xx + sp + r
    cy = base1 - cap * 0.42
    d.ellipse((cx - r, cy - r, cx + r, cy + r), fill=color)
    put(lines[1], cx + r + sp, base1)
    put(lines[2], x, base2)


def save(im, path):
    im.save(f"{OUT}/{path}", optimize=True)


# ---------------------------------------------------------------- light logo
WHITE = (255, 255, 255)
L = (40, 22, 945, 368)                     # board box of the light logo
MARKW = (395 - L[0]) * S
light = crop(*L)
lmask = with_mark_mask(light, MARKW)
lt = transparent(light, WHITE, lmask)
save(trim(lt), "logos/leassh-logo-clair-en-transparent.png")
save(on((255, 255, 255, 255), trim(lt, 40)), "logos/leassh-logo-clair-en-fond-blanc.png")

# FR: erase English baseline and redraw it in French
fr = erase(light, ((460 - L[0]) * S, (238 - L[1]) * S, (935 - L[0]) * S, (326 - L[1]) * S))
need = (466 - L[0]) * S + tag_width(27 * S) + 30 * S - fr.shape[1]
if need > 0:
    fr = pad_right(fr, int(need))
img = Image.fromarray(fr)
draw_tag(img, (466 - L[0]) * S, (272 - L[1]) * S, (311 - L[1]) * S, 27 * S, (7, 15, 90))
frm = np.zeros(fr.shape[:2]); frm[:, :lmask.shape[1]] = lmask
ft = transparent(np.array(img.convert("RGB")), WHITE, frm)
save(trim(ft), "logos/leassh-logo-clair-fr-transparent.png")
save(on((255, 255, 255, 255), trim(ft, 40)), "logos/leassh-logo-clair-fr-fond-blanc.png")

# symbol + wordmark (light)
sym_t = transparent(light[:, :MARKW], WHITE, lmask[:, :MARKW])
save(trim(sym_t), "symboles/leassh-symbole-couleur-transparent.png")
save(on((255, 255, 255, 255), trim(sym_t, 40)), "symboles/leassh-symbole-couleur-fond-blanc.png")
W = (455, 100, 935, 228)
save(trim(transparent(crop(*W), WHITE)), "logos/leassh-nom-seul-transparent.png")

# ---------------------------------------------------------------- dark logo
D = (15, 389, 822, 651)
dark = crop(*D)
NAVY = tuple(int(v) for v in np.median(dark[6 * S:21 * S, 245 * S:285 * S].reshape(-1, 3), 0))
# paint the area outside the band's rounded corners with the band colour
_m = Image.new("L", (dark.shape[1] + 800 * S, dark.shape[0]), 0)
ImageDraw.Draw(_m).rounded_rectangle((3 * S, 3 * S, _m.width - 1, _m.height - 1 - 3 * S), 22 * S, fill=255)
_m = np.array(_m)[:, :dark.shape[1]] > 0
dark[~_m] = NAVY
def rounded(rgb, r):
    im = Image.fromarray(rgb).convert("RGBA")
    m = Image.new("L", im.size, 0)
    ImageDraw.Draw(m).rounded_rectangle((0, 0, im.width - 1, im.height - 1), r, fill=255)
    im.putalpha(m)
    return im
# extend right side with background so the cut is clean, then round all corners
save(rounded(dark, 24 * S), "logos/leassh-logo-sombre-en.png")
dfr = erase(dark, ((385 - D[0]) * S, (552 - D[1]) * S, (815 - D[0]) * S, (628 - D[1]) * S))
need = (391 - D[0]) * S + tag_width(22 * S) + 40 * S - dfr.shape[1]
if need > 0:
    dfr = pad_right(dfr, int(need))
img = Image.fromarray(dfr)
draw_tag(img, (391 - D[0]) * S, (582 - D[1]) * S, (616 - D[1]) * S, 22 * S, (219, 221, 242))
save(rounded(np.array(img.convert("RGB")), 24 * S), "logos/leassh-logo-sombre-fr.png")

# transparent versions of the dark logo (for dark backgrounds)
DMW = (330 - D[0]) * S
dm = with_mark_mask(dark, DMW)
save(trim(transparent(dark, NAVY, dm, floor=0.08, lighter=True)), "logos/leassh-logo-sombre-en-transparent.png")
frgb = np.array(img.convert("RGB"))
dm2 = np.zeros(frgb.shape[:2]); dm2[:, :DMW] = dm[:, :DMW]
save(trim(transparent(frgb, NAVY, dm2, floor=0.08, lighter=True)), "logos/leassh-logo-sombre-fr-transparent.png")

# ---------------------------------------------------------------- app icon
A = (1053, 51, 1298, 295)
icon = crop(*A)
im = Image.fromarray(icon).convert("RGBA")
# rounded-square mask fitted on the board icon
mask = Image.new("L", im.size, 0)
ib = (3 * S, 3 * S, im.width - 3 * S, im.height - 4 * S)
ImageDraw.Draw(mask).rounded_rectangle(ib, 45 * S, fill=255)
mask = mask.filter(ImageFilter.GaussianBlur(1.2))
im.putalpha(mask)
im = im.crop(ib)
im = im.resize((1024, 1024), Image.LANCZOS)
save(im, "icone/leassh-icone-1024.png")
for s in (512, 256, 192, 180, 128, 64, 48, 32, 16):
    save(im.resize((s, s), Image.LANCZOS), f"icone/leassh-icone-{s}.png")
im.resize((256, 256), Image.LANCZOS).save(f"{OUT}/favicon/favicon.ico", sizes=[(16, 16), (32, 32), (48, 48), (64, 64)])
im.resize((32, 32), Image.LANCZOS).save(f"{OUT}/favicon/favicon-32x32.png")
im.resize((16, 16), Image.LANCZOS).save(f"{OUT}/favicon/favicon-16x16.png")
im.resize((180, 180), Image.LANCZOS).save(f"{OUT}/favicon/apple-touch-icon.png")
im.resize((192, 192), Image.LANCZOS).save(f"{OUT}/favicon/android-chrome-192x192.png")
im.resize((512, 512), Image.LANCZOS).save(f"{OUT}/favicon/android-chrome-512x512.png")

# ---------------------------------------------------------------- mono / outline cards
for name, box, tag_color, base, markx in (
        ("monochrome", (36, 688, 470, 850), (12, 12, 12), (223, 806, 826), 212),
        ("contour", (520, 688, 945, 850), (16, 29, 153), (702, 806, 826), 694)):
    rgb = crop(*box)
    cardbg = tuple(int(v) for v in np.median(rgb[4:20, 4:20].reshape(-1, 3), 0))
    mw = (markx - box[0]) * S
    m = with_mark_mask(rgb, mw)
    t = transparent(rgb, cardbg, m)
    save(trim(t), f"logos/leassh-logo-{name}-en-transparent.png")
    save(on((255, 255, 255, 255), trim(t, 30)), f"logos/leassh-logo-{name}-en-fond-blanc.png")
    sm = transparent(rgb[:, :mw], cardbg, m[:, :mw])
    save(trim(sm), f"symboles/leassh-symbole-{name}-transparent.png")
    x, b1, b2 = base
    e = erase(rgb, ((x - 4 - box[0]) * S, (790 - box[1]) * S, (box[2] - 2 - box[0]) * S, (834 - box[1]) * S))
    need = (x - box[0]) * S + tag_width(13 * S) + 12 * S - e.shape[1]
    if need > 0:
        e = pad_right(e, int(need))
    img = Image.fromarray(e)
    draw_tag(img, (x - box[0]) * S, (b1 - box[1]) * S, (b2 - box[1]) * S, 13 * S, tag_color)
    m2 = np.zeros(e.shape[:2]); m2[:, :mw] = m[:, :mw]
    t = transparent(np.array(img.convert("RGB")), cardbg, m2)
    save(trim(t), f"logos/leassh-logo-{name}-fr-transparent.png")
    save(on((255, 255, 255, 255), trim(t, 30)), f"logos/leassh-logo-{name}-fr-fond-blanc.png")
print("ok")

import json
json.dump({"name": "LeaSSH", "short_name": "LeaSSH", "icons": [
    {"src": "/android-chrome-192x192.png", "sizes": "192x192", "type": "image/png"},
    {"src": "/android-chrome-512x512.png", "sizes": "512x512", "type": "image/png"}],
    "theme_color": "#00114C", "background_color": "#00114C", "display": "standalone"},
    open(f"{OUT}/favicon/site.webmanifest", "w"), indent=2)
os.makedirs(f"{OUT}/source", exist_ok=True)
shutil.copy("src/ref.png", f"{OUT}/source/planche-originale.png")
shutil.copy("gen/README-png.md", f"{OUT}/README.md")
