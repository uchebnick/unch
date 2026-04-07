#!/usr/bin/env python3
from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path
from typing import Iterable

from PIL import Image, ImageColor, ImageDraw, ImageFilter, ImageFont


ROOT = Path(__file__).resolve().parents[1]
ASSETS_DIR = ROOT / "docs" / "assets"
GIF_PATH = ASSETS_DIR / "unch-demo.gif"
SVG_PATH = ASSETS_DIR / "unch-demo.svg"

FONT_PATH = Path("/System/Library/Fonts/Menlo.ttc")
MONO_STACK = "SFMono-Regular, Menlo, Consolas, monospace"

IMAGE_W = 1200
IMAGE_H = 760

BG = "#09111F"
WINDOW_BG = "#0F172A"
WINDOW_BORDER = "#1F2A44"
TITLE_BG = "#111B32"
BOX_BG = "#0B1324"
BOX_BORDER = "#24324D"
BOX_HIGHLIGHT = "#17243E"
BOX_ACCENT = "#38BDF8"
PROMPT = "#93C5FD"
TEXT = "#E5E7EB"
SUCCESS = "#A7F3D0"
MUTED = "#CBD5E1"
CAPTION = "#64748B"
CURSOR = "#7DD3FC"
SHADOW = "#020617"

WINDOW_X = 32
WINDOW_Y = 32
WINDOW_W = 1136
WINDOW_H = 696
WINDOW_R = 20

TITLE_H = 44
TITLE_SIZE = 18
TERMINAL_SIZE = 24
CAPTION_SIZE = 20
CAPTION_Y = 718

BOX1_X = 72
BOX1_Y = 294
BOX1_W = 1056
BOX1_H = 156

BOX2_X = 72
BOX2_Y = 474
BOX2_W = 1056
BOX2_H = 194


@dataclass(frozen=True)
class Line:
    text: str
    fill: str = TEXT
    x: int = 104
    y: int = 0
    show_cursor: bool = False


@dataclass(frozen=True)
class RevealLine:
    text: str
    x: int
    y: int
    fill: str
    progress: float


def load_font(size: int) -> ImageFont.FreeTypeFont:
    return ImageFont.truetype(str(FONT_PATH), size=size)


def text_width(draw: ImageDraw.ImageDraw, text: str, font: ImageFont.ImageFont) -> float:
    if not text:
        return 0
    return draw.textlength(text, font=font)


def fit_text(draw: ImageDraw.ImageDraw, text: str, font: ImageFont.ImageFont, max_width: int) -> str:
    text = text or ""
    if max_width <= 0 or text_width(draw, text, font) <= max_width:
        return text

    ellipsis = "..."
    if text_width(draw, ellipsis, font) >= max_width:
        return ""

    lo = 0
    hi = len(text)
    best = ellipsis
    while lo <= hi:
        mid = (lo + hi) // 2
        candidate = text[:mid].rstrip() + ellipsis
        if text_width(draw, candidate, font) <= max_width:
            best = candidate
            lo = mid + 1
        else:
            hi = mid - 1
    return best


def rgba(color: str, alpha: int) -> tuple[int, int, int, int]:
    r, g, b = ImageColor.getrgb(color)
    return (r, g, b, max(0, min(255, alpha)))


def ease_out(progress: float) -> float:
    progress = max(0.0, min(1.0, progress))
    return 1.0 - (1.0 - progress) ** 3


def draw_cursor(draw: ImageDraw.ImageDraw, x: float, y: int, font_size: int, alpha: int = 255) -> None:
    top = y - font_size + 3
    bottom = y + 1
    left = int(round(x + 3))
    right = left + 5
    draw.rounded_rectangle((left, top, right, bottom), radius=2, fill=rgba(CURSOR, int(alpha * 0.92)))
    draw.rounded_rectangle((left + 1, top + 1, right - 1, bottom - 1), radius=1, fill=rgba("#C7EEFF", int(alpha * 0.35)))


def add_glow(image: Image.Image, bbox: tuple[int, int, int, int], color: str, alpha: int, blur: int) -> None:
    overlay = Image.new("RGBA", image.size, (0, 0, 0, 0))
    glow = ImageDraw.Draw(overlay)
    glow.ellipse(bbox, fill=rgba(color, alpha))
    overlay = overlay.filter(ImageFilter.GaussianBlur(blur))
    image.alpha_composite(overlay)


def draw_shadowed_text(
    draw: ImageDraw.ImageDraw,
    x: int,
    y: int,
    text: str,
    font: ImageFont.ImageFont,
    fill: str,
    alpha: int = 255,
    shadow_alpha: int = 80,
    dy: int = 0,
) -> None:
    if alpha <= 0:
        return
    draw.text((x, y + dy + 1), text, fill=rgba(SHADOW, min(alpha, shadow_alpha)), font=font)
    draw.text((x, y + dy), text, fill=rgba(fill, alpha), font=font)


def draw_reveal_line(draw: ImageDraw.ImageDraw, line: RevealLine, font: ImageFont.ImageFont) -> None:
    if line.progress <= 0:
        return
    eased = ease_out(line.progress)
    alpha = int(255 * eased)
    dy = int(round((1.0 - eased) * 10))
    draw_shadowed_text(draw, line.x, line.y - TERMINAL_SIZE, line.text, font, line.fill, alpha=alpha, dy=dy)


def base_image() -> Image.Image:
    image = Image.new("RGBA", (IMAGE_W, IMAGE_H), BG)
    add_glow(image, (-120, -80, 420, 280), "#0EA5E9", 46, 70)
    add_glow(image, (760, 60, 1280, 420), "#22C55E", 34, 92)
    add_glow(image, (820, 420, 1320, 900), "#2563EB", 30, 80)

    shadow = Image.new("RGBA", image.size, (0, 0, 0, 0))
    shadow_draw = ImageDraw.Draw(shadow)
    shadow_draw.rounded_rectangle(
        (WINDOW_X, WINDOW_Y + 12, WINDOW_X + WINDOW_W, WINDOW_Y + WINDOW_H + 12),
        radius=WINDOW_R + 2,
        fill=rgba(SHADOW, 120),
    )
    shadow = shadow.filter(ImageFilter.GaussianBlur(20))
    image.alpha_composite(shadow)

    draw = ImageDraw.Draw(image)
    draw.rounded_rectangle(
        (WINDOW_X, WINDOW_Y, WINDOW_X + WINDOW_W, WINDOW_Y + WINDOW_H),
        radius=WINDOW_R,
        fill=WINDOW_BG,
        outline=WINDOW_BORDER,
        width=2,
    )
    draw.rounded_rectangle(
        (WINDOW_X, WINDOW_Y, WINDOW_X + WINDOW_W, WINDOW_Y + TITLE_H),
        radius=WINDOW_R,
        fill=TITLE_BG,
    )
    draw.line((WINDOW_X + 1, WINDOW_Y + TITLE_H, WINDOW_X + WINDOW_W - 1, WINDOW_Y + TITLE_H), fill=rgba("#223456", 200), width=1)

    for cx, color in ((68, "#F87171"), (96, "#FBBF24"), (124, "#34D399")):
        draw.ellipse((cx - 8, 54 - 8, cx + 8, 54 + 8), fill=color)

    title_font = load_font(TITLE_SIZE)
    title = "unch demo"
    title_x = IMAGE_W / 2 - text_width(draw, title, title_font) / 2
    draw.text((title_x, 42), title, fill=TEXT, font=title_font)
    return image


def draw_prompt_line(
    draw: ImageDraw.ImageDraw,
    y: int,
    text: str,
    font: ImageFont.ImageFont,
    show_cursor: bool = False,
    x_prompt: int = 72,
    x_text: int = 104,
    alpha: int = 255,
    max_text_width: int | None = None,
) -> None:
    if max_text_width is not None:
        text = fit_text(draw, text, font, max_text_width)
    draw_shadowed_text(draw, x_prompt, y - TERMINAL_SIZE, "$", font, PROMPT, alpha=alpha)
    draw_shadowed_text(draw, x_text, y - TERMINAL_SIZE, text, font, TEXT, alpha=alpha)
    if show_cursor:
        cursor_x = x_text + text_width(draw, text, font)
        draw_cursor(draw, cursor_x, y, TERMINAL_SIZE, alpha=alpha)


def draw_lines(draw: ImageDraw.ImageDraw, lines: Iterable[Line], font: ImageFont.ImageFont) -> None:
    for line in lines:
        draw_shadowed_text(draw, line.x, line.y - TERMINAL_SIZE, line.text, font, line.fill)
        if line.show_cursor:
            cursor_x = line.x + text_width(draw, line.text, font)
            draw_cursor(draw, cursor_x, line.y, TERMINAL_SIZE)


def draw_box(image: Image.Image, x: int, y: int, w: int, h: int, progress: float = 1.0) -> None:
    if progress <= 0:
        return

    eased = ease_out(progress)
    alpha = int(255 * eased)
    dy = int(round((1.0 - eased) * 8))

    shadow = Image.new("RGBA", image.size, (0, 0, 0, 0))
    shadow_draw = ImageDraw.Draw(shadow)
    shadow_draw.rounded_rectangle(
        (x, y + 10 + dy, x + w, y + h + 10 + dy),
        radius=18,
        fill=rgba(SHADOW, int(105 * eased)),
    )
    shadow = shadow.filter(ImageFilter.GaussianBlur(16))
    image.alpha_composite(shadow)

    overlay = Image.new("RGBA", image.size, (0, 0, 0, 0))
    draw = ImageDraw.Draw(overlay)
    draw.rounded_rectangle((x, y + dy, x + w, y + h + dy), radius=16, fill=rgba(BOX_BG, alpha), outline=rgba(BOX_BORDER, alpha), width=1)
    draw.rounded_rectangle((x + 1, y + 1 + dy, x + w - 1, y + 26 + dy), radius=15, fill=rgba(BOX_HIGHLIGHT, int(72 * eased)))
    image.alpha_composite(overlay)


def frame(
    *,
    cd_text: str,
    index_text: str,
    show_loaded: bool,
    show_indexed: bool,
    search1_text: str = "",
    search1_result_progress: tuple[float, float] = (0.0, 0.0),
    search2_text: str = "",
    search2_result_progress: tuple[float, float, float, float] = (0.0, 0.0, 0.0, 0.0),
    box1_progress: float = 0.0,
    box2_progress: float = 0.0,
    cursor_target: str | None = None,
) -> Image.Image:
    image = base_image()
    draw = ImageDraw.Draw(image)
    terminal_font = load_font(TERMINAL_SIZE)
    caption_font = load_font(CAPTION_SIZE)

    draw_prompt_line(draw, 122, cd_text, terminal_font, show_cursor=cursor_target == "cd")
    draw_prompt_line(draw, 176, index_text, terminal_font, show_cursor=cursor_target == "index")

    lines: list[Line] = []
    if show_loaded:
        lines.append(Line("Loaded model       dim=768", fill=SUCCESS, y=218))
    if show_indexed:
        lines.append(Line("Indexed 278 symbols in 16 files", fill=SUCCESS, y=256))
    draw_lines(draw, lines, terminal_font)

    if box1_progress > 0:
        draw_box(image, BOX1_X, BOX1_Y, BOX1_W, BOX1_H, progress=box1_progress)
        draw_prompt_line(
            draw,
            338,
            f'unch search "{search1_text}',
            terminal_font,
            show_cursor=cursor_target == "search1",
            x_prompt=96,
            x_text=128,
            alpha=int(255 * ease_out(box1_progress)),
            max_text_width=BOX1_W - (128 - BOX1_X) - 28,
        )
        draw_reveal_line(draw, RevealLine("1. mux.go:32   0.7747", 96, 382, TEXT, search1_result_progress[0]), terminal_font)
        draw_reveal_line(draw, RevealLine("2. mux.go:314  0.8135", 96, 418, MUTED, search1_result_progress[1]), terminal_font)

    if box2_progress > 0:
        draw_box(image, BOX2_X, BOX2_Y, BOX2_W, BOX2_H, progress=box2_progress)
        draw_prompt_line(
            draw,
            522,
            f'unch search --details "{search2_text}',
            terminal_font,
            show_cursor=cursor_target == "search2",
            x_prompt=96,
            x_text=128,
            alpha=int(255 * ease_out(box2_progress)),
            max_text_width=BOX2_W - (128 - BOX2_X) - 28,
        )
        draw_reveal_line(draw, RevealLine("1. mux.go:466   0.7991", 96, 558, TEXT, search2_result_progress[0]), terminal_font)
        draw_reveal_line(draw, RevealLine("kind: function", 128, 592, MUTED, search2_result_progress[1]), terminal_font)
        draw_reveal_line(draw, RevealLine("name: Vars", 128, 624, MUTED, search2_result_progress[2]), terminal_font)
        draw_reveal_line(
            draw,
            RevealLine("signature: func Vars(r *http.Request) map[string]string", 128, 656, MUTED, search2_result_progress[3]),
            terminal_font,
        )

    caption = "Real output captured from indexing github.com/gorilla/mux with unch."
    draw_shadowed_text(draw, 72, CAPTION_Y - CAPTION_SIZE, caption, caption_font, CAPTION, alpha=235, shadow_alpha=50)
    return image.convert("P", palette=Image.ADAPTIVE)


def append_typed_frames(
    frames: list[Image.Image],
    durations: list[int],
    *,
    base_kwargs: dict,
    target_key: str,
    full_text: str,
    cursor_name: str,
    hold_ms: int = 70,
    step: int = 1,
) -> None:
    indices = list(range(0, len(full_text) + 1, max(1, step)))
    if indices[-1] != len(full_text):
        indices.append(len(full_text))
    for i in indices:
        kwargs = dict(base_kwargs)
        kwargs[target_key] = full_text[:i]
        kwargs["cursor_target"] = cursor_name
        frames.append(frame(**kwargs))
        durations.append(hold_ms)


def hold(frames: list[Image.Image], durations: list[int], count: int, ms: int, **kwargs) -> None:
    for _ in range(count):
        frames.append(frame(**kwargs))
        durations.append(ms)


def transition(
    frames: list[Image.Image],
    durations: list[int],
    steps: int,
    ms: int,
    updater,
    base_kwargs: dict,
) -> None:
    for idx in range(steps):
        progress = (idx + 1) / steps
        kwargs = dict(base_kwargs)
        updater(kwargs, progress)
        frames.append(frame(**kwargs))
        durations.append(ms)


def build_gif() -> None:
    frames: list[Image.Image] = []
    durations: list[int] = []

    hold(
        frames,
        durations,
        6,
        70,
        cd_text="",
        index_text="",
        show_loaded=False,
        show_indexed=False,
        box1_progress=0.0,
        box2_progress=0.0,
        cursor_target="cd",
    )
    append_typed_frames(
        frames,
        durations,
        base_kwargs=dict(cd_text="", index_text="", show_loaded=False, show_indexed=False, box1_progress=0.0, box2_progress=0.0),
        target_key="cd_text",
        full_text="cd gorilla/mux",
        cursor_name="cd",
    )
    hold(
        frames,
        durations,
        6,
        70,
        cd_text="cd gorilla/mux",
        index_text="",
        show_loaded=False,
        show_indexed=False,
        box1_progress=0.0,
        box2_progress=0.0,
        cursor_target="index",
    )
    append_typed_frames(
        frames,
        durations,
        base_kwargs=dict(cd_text="cd gorilla/mux", index_text="", show_loaded=False, show_indexed=False, box1_progress=0.0, box2_progress=0.0),
        target_key="index_text",
        full_text="unch index --root .",
        cursor_name="index",
    )
    hold(
        frames,
        durations,
        6,
        80,
        cd_text="cd gorilla/mux",
        index_text="unch index --root .",
        show_loaded=True,
        show_indexed=False,
        box1_progress=0.0,
        box2_progress=0.0,
    )
    hold(
        frames,
        durations,
        10,
        80,
        cd_text="cd gorilla/mux",
        index_text="unch index --root .",
        show_loaded=True,
        show_indexed=True,
        box1_progress=0.0,
        box2_progress=0.0,
    )
    transition(
        frames,
        durations,
        5,
        70,
        lambda kwargs, progress: kwargs.update(box1_progress=progress, cursor_target="search1"),
        dict(
            cd_text="cd gorilla/mux",
            index_text="unch index --root .",
            show_loaded=True,
            show_indexed=True,
            box1_progress=0.0,
            box2_progress=0.0,
        ),
    )
    append_typed_frames(
        frames,
        durations,
        base_kwargs=dict(
            cd_text="cd gorilla/mux",
            index_text="unch index --root .",
            show_loaded=True,
            show_indexed=True,
            box1_progress=1.0,
            box2_progress=0.0,
            search1_text="",
        ),
        target_key="search1_text",
        full_text="create a new router\"",
        cursor_name="search1",
        hold_ms=65,
        step=2,
    )
    transition(
        frames,
        durations,
        5,
        80,
        lambda kwargs, progress: kwargs.update(search1_result_progress=(progress, 0.0)),
        dict(
            cd_text="cd gorilla/mux",
            index_text="unch index --root .",
            show_loaded=True,
            show_indexed=True,
            box1_progress=1.0,
            box2_progress=0.0,
            search1_text='create a new router"',
        ),
    )
    transition(
        frames,
        durations,
        5,
        80,
        lambda kwargs, progress: kwargs.update(search1_result_progress=(1.0, progress)),
        dict(
            cd_text="cd gorilla/mux",
            index_text="unch index --root .",
            show_loaded=True,
            show_indexed=True,
            box1_progress=1.0,
            box2_progress=0.0,
            search1_text='create a new router"',
        ),
    )
    hold(
        frames,
        durations,
        6,
        85,
        cd_text="cd gorilla/mux",
        index_text="unch index --root .",
        show_loaded=True,
        show_indexed=True,
        box1_progress=1.0,
        box2_progress=0.0,
        search1_text='create a new router"',
        search1_result_progress=(1.0, 1.0),
    )
    transition(
        frames,
        durations,
        5,
        70,
        lambda kwargs, progress: kwargs.update(box2_progress=progress, cursor_target="search2"),
        dict(
            cd_text="cd gorilla/mux",
            index_text="unch index --root .",
            show_loaded=True,
            show_indexed=True,
            box1_progress=1.0,
            box2_progress=0.0,
            search1_text='create a new router"',
            search1_result_progress=(1.0, 1.0),
        ),
    )
    append_typed_frames(
        frames,
        durations,
        base_kwargs=dict(
            cd_text="cd gorilla/mux",
            index_text="unch index --root .",
            show_loaded=True,
            show_indexed=True,
            box1_progress=1.0,
            search1_text='create a new router"',
            search1_result_progress=(1.0, 1.0),
            box2_progress=1.0,
            search2_text="",
        ),
        target_key="search2_text",
        full_text='get path variables from a request"',
        cursor_name="search2",
        hold_ms=60,
        step=2,
    )
    for index in range(4):
        transition(
            frames,
            durations,
            4,
            85,
            lambda kwargs, progress, idx=index: kwargs.update(
                search2_result_progress=tuple(1.0 if i < idx else progress if i == idx else 0.0 for i in range(4))
            ),
            dict(
                cd_text="cd gorilla/mux",
                index_text="unch index --root .",
                show_loaded=True,
                show_indexed=True,
                box1_progress=1.0,
                box2_progress=1.0,
                search1_text='create a new router"',
                search1_result_progress=(1.0, 1.0),
                search2_text='get path variables from a request"',
            ),
        )
    hold(
        frames,
        durations,
        18,
        95,
        cd_text="cd gorilla/mux",
        index_text="unch index --root .",
        show_loaded=True,
        show_indexed=True,
        box1_progress=1.0,
        search1_text='create a new router"',
        search1_result_progress=(1.0, 1.0),
        box2_progress=1.0,
        search2_text='get path variables from a request"',
        search2_result_progress=(1.0, 1.0, 1.0, 1.0),
    )

    frames[0].save(
        GIF_PATH,
        save_all=True,
        append_images=frames[1:],
        duration=durations,
        loop=0,
        optimize=True,
        disposal=2,
    )


def build_svg() -> None:
    terminal_size = TERMINAL_SIZE
    svg = f"""<svg xmlns="http://www.w3.org/2000/svg" width="{IMAGE_W}" height="{IMAGE_H}" viewBox="0 0 {IMAGE_W} {IMAGE_H}" fill="none">
  <defs>
    <radialGradient id="glowA" cx="0" cy="0" r="1" gradientUnits="userSpaceOnUse" gradientTransform="translate(140 70) rotate(26) scale(360 220)">
      <stop stop-color="#0EA5E9" stop-opacity="0.20"/>
      <stop offset="1" stop-color="#0EA5E9" stop-opacity="0"/>
    </radialGradient>
    <radialGradient id="glowB" cx="0" cy="0" r="1" gradientUnits="userSpaceOnUse" gradientTransform="translate(1000 180) rotate(38) scale(340 240)">
      <stop stop-color="#22C55E" stop-opacity="0.14"/>
      <stop offset="1" stop-color="#22C55E" stop-opacity="0"/>
    </radialGradient>
    <linearGradient id="windowGrad" x1="600" y1="32" x2="600" y2="728" gradientUnits="userSpaceOnUse">
      <stop stop-color="#101A2E"/>
      <stop offset="1" stop-color="#0D1628"/>
    </linearGradient>
    <linearGradient id="panelGrad" x1="600" y1="294" x2="600" y2="656" gradientUnits="userSpaceOnUse">
      <stop stop-color="#101A2F"/>
      <stop offset="1" stop-color="#0A1325"/>
    </linearGradient>
    <filter id="shadowLg" x="-20%" y="-20%" width="140%" height="160%">
      <feDropShadow dx="0" dy="12" stdDeviation="18" flood-color="#020617" flood-opacity="0.45"/>
    </filter>
    <filter id="shadowSm" x="-20%" y="-20%" width="140%" height="180%">
      <feDropShadow dx="0" dy="10" stdDeviation="12" flood-color="#020617" flood-opacity="0.38"/>
    </filter>
  </defs>
  <rect width="{IMAGE_W}" height="{IMAGE_H}" rx="28" fill="{BG}"/>
  <rect width="{IMAGE_W}" height="{IMAGE_H}" rx="28" fill="url(#glowA)"/>
  <rect width="{IMAGE_W}" height="{IMAGE_H}" rx="28" fill="url(#glowB)"/>
  <rect x="{WINDOW_X}" y="{WINDOW_Y}" width="{WINDOW_W}" height="{WINDOW_H}" rx="{WINDOW_R}" fill="url(#windowGrad)" stroke="{WINDOW_BORDER}" stroke-width="2" filter="url(#shadowLg)"/>
  <rect x="{WINDOW_X}" y="{WINDOW_Y}" width="{WINDOW_W}" height="{TITLE_H}" rx="{WINDOW_R}" fill="{TITLE_BG}"/>
  <line x1="{WINDOW_X + 1}" y1="{WINDOW_Y + TITLE_H}" x2="{WINDOW_X + WINDOW_W - 1}" y2="{WINDOW_Y + TITLE_H}" stroke="#223456"/>
  <circle cx="68" cy="54" r="8" fill="#F87171"/>
  <circle cx="96" cy="54" r="8" fill="#FBBF24"/>
  <circle cx="124" cy="54" r="8" fill="#34D399"/>
  <text x="568" y="60" text-anchor="middle" fill="{TEXT}" font-family="{MONO_STACK}" font-size="{TITLE_SIZE}">unch demo</text>

  <text x="72" y="122" fill="{PROMPT}" font-family="{MONO_STACK}" font-size="{terminal_size}">$</text>
  <text x="104" y="122" fill="{TEXT}" font-family="{MONO_STACK}" font-size="{terminal_size}">cd gorilla/mux</text>

  <text x="72" y="176" fill="{PROMPT}" font-family="{MONO_STACK}" font-size="{terminal_size}">$</text>
  <text x="104" y="176" fill="{TEXT}" font-family="{MONO_STACK}" font-size="{terminal_size}">unch index --root .</text>
  <text x="104" y="218" fill="{SUCCESS}" font-family="{MONO_STACK}" font-size="{terminal_size}">Loaded model       dim=768</text>
  <text x="104" y="256" fill="{SUCCESS}" font-family="{MONO_STACK}" font-size="{terminal_size}">Indexed 278 symbols in 16 files</text>

  <rect x="{BOX1_X}" y="{BOX1_Y}" width="{BOX1_W}" height="{BOX1_H}" rx="16" fill="url(#panelGrad)" stroke="{BOX_BORDER}" filter="url(#shadowSm)"/>
  <rect x="{BOX1_X + 1}" y="{BOX1_Y + 1}" width="{BOX1_W - 2}" height="26" rx="15" fill="{BOX_HIGHLIGHT}" fill-opacity="0.55"/>
  <text x="96" y="338" fill="{PROMPT}" font-family="{MONO_STACK}" font-size="{terminal_size}">$</text>
  <text x="128" y="338" fill="{TEXT}" font-family="{MONO_STACK}" font-size="{terminal_size}">unch search "create a new router"</text>
  <text x="96" y="382" fill="{TEXT}" font-family="{MONO_STACK}" font-size="{terminal_size}">1. mux.go:32   0.7747</text>
  <text x="96" y="418" fill="{MUTED}" font-family="{MONO_STACK}" font-size="{terminal_size}">2. mux.go:314  0.8135</text>

  <rect x="{BOX2_X}" y="{BOX2_Y}" width="{BOX2_W}" height="{BOX2_H}" rx="16" fill="url(#panelGrad)" stroke="{BOX_BORDER}" filter="url(#shadowSm)"/>
  <rect x="{BOX2_X + 1}" y="{BOX2_Y + 1}" width="{BOX2_W - 2}" height="26" rx="15" fill="{BOX_HIGHLIGHT}" fill-opacity="0.55"/>
  <text x="96" y="522" fill="{PROMPT}" font-family="{MONO_STACK}" font-size="{terminal_size}">$</text>
  <text x="128" y="522" fill="{TEXT}" font-family="{MONO_STACK}" font-size="{terminal_size}">unch search --details "get path variables from a request"</text>
  <text x="96" y="558" fill="{TEXT}" font-family="{MONO_STACK}" font-size="{terminal_size}">1. mux.go:466   0.7991</text>
  <text x="128" y="592" fill="{MUTED}" font-family="{MONO_STACK}" font-size="{terminal_size}">kind: function</text>
  <text x="128" y="624" fill="{MUTED}" font-family="{MONO_STACK}" font-size="{terminal_size}">name: Vars</text>
  <text x="128" y="656" fill="{MUTED}" font-family="{MONO_STACK}" font-size="{terminal_size}">signature: func Vars(r *http.Request) map[string]string</text>

  <text x="72" y="{CAPTION_Y}" fill="{CAPTION}" font-family="{MONO_STACK}" font-size="{CAPTION_SIZE}">Real output captured from indexing github.com/gorilla/mux with unch.</text>
</svg>
"""
    SVG_PATH.write_text(svg, encoding="utf-8")


def main() -> None:
    ASSETS_DIR.mkdir(parents=True, exist_ok=True)
    build_gif()
    build_svg()


if __name__ == "__main__":
    main()
