"""Render the visible original and accepted answers onto fresh, non-interactive PDF pages."""
import json
import math
from pathlib import Path
import sys

import cairo
import gi

gi.require_version("Poppler", "0.18")
gi.require_version("PangoCairo", "1.0")
from gi.repository import Pango, PangoCairo, Poppler


def draw_text(ctx, text, signature, width, height):
    layout = PangoCairo.create_layout(ctx)
    font = Pango.FontDescription.from_string("Serif Italic" if signature else "Sans")
    font.set_absolute_size(14 * Pango.SCALE)
    layout.set_font_description(font)
    layout.set_text(text, -1)
    layout.set_wrap(Pango.WrapMode.WORD_CHAR)
    # Wrap at a width proportional to the field. Scale uniformly to retain all lines.
    scale = min(1, height / 18)
    layout.set_width(min(2**31 - 1, max(1, int(width / scale * Pango.SCALE))))
    ink, logical = layout.get_extents()
    left = min(ink.x, logical.x) / Pango.SCALE
    right = max(ink.x + ink.width, logical.x + logical.width) / Pango.SCALE
    top = min(ink.y, logical.y) / Pango.SCALE
    bottom = max(ink.y + ink.height, logical.y + logical.height) / Pango.SCALE
    scale = min(scale, width / max(right - left, 1), height / max(bottom - top, 1))
    ctx.translate(-left * scale, (height - (bottom - top) * scale) / 2 - top * scale)
    ctx.scale(scale, scale)
    PangoCairo.show_layout(ctx, layout)


def draw(ctx, field, page_width, page_height):
    width = field["width"] * page_width
    height = field["height"] * page_height
    ctx.save()
    ctx.translate(field["x"] * page_width, field["y"] * page_height)
    ctx.rectangle(0, 0, width, height)
    ctx.clip()
    ctx.set_source_rgb(0.06, 0.09, 0.12)
    if field["image"]:
        image = cairo.ImageSurface.create_from_png(field["image"])
        scale = min(width / image.get_width(), height / image.get_height())
        ctx.translate((width - image.get_width() * scale) / 2,
                      (height - image.get_height() * scale) / 2)
        ctx.scale(scale, scale)
        ctx.set_source_surface(image, 0, 0)
        ctx.paint()
        image.finish()
    elif field["type"] == "checkbox":
        size = min(width, height)
        ctx.translate((width - size) / 2, (height - size) / 2)
        ctx.scale(size, size)
        ctx.set_line_width(0.065)
        ctx.rectangle(.06, .06, .88, .88)
        ctx.stroke()
        if field["checked"]:
            ctx.set_line_width(.1)
            ctx.move_to(.19, .48)
            ctx.line_to(.42, .73)
            ctx.line_to(.82, .25)
            ctx.stroke()
    else:
        draw_text(ctx, field["text"], field["type"] == "signature", width, height)
    ctx.restore()


def main():
    manifest = json.load(sys.stdin)
    doc = Poppler.Document.new_from_file(Path("original.pdf").resolve().as_uri(), None)
    if doc.get_n_pages() != manifest["pages"]:
        raise ValueError("Unexpected original page count")
    surface = cairo.PDFSurface("result.pdf", 1, 1)
    by_page = {}
    for field in manifest["fields"]:
        by_page.setdefault(field["page"], []).append(field)
    for index in range(doc.get_n_pages()):
        page = doc.get_page(index)
        width, height = page.get_size()
        if not all(math.isfinite(v) and v > 0 for v in (width, height)):
            raise ValueError("Invalid page size")
        surface.set_size(width, height)
        ctx = cairo.Context(surface)
        # Poppler's display coordinates already account for crop and page rotation.
        ctx.save()
        page.render(ctx)
        ctx.restore()
        for field in by_page.get(index + 1, ()):
            draw(ctx, field, width, height)
        ctx.show_page()
    surface.finish()


if __name__ == "__main__":
    main()
