---
name: slides-design
description: Design clear, accessible 16:9 slide decks with python-pptx — layout grid, type scale, colour tokens, native charts, tables, and a visual QA checklist.
---

# Slide design system

Use this together with `office-pptx` (mechanics and `python-pptx` API) and, for
medical or pharmaceutical content, `medical-slides`. This skill covers the
visual system: grid, type, colour, charts, tables and the checks that catch an
unpolished deck before delivery.

## Set the system once

Create a helper module in the workspace (for example `deck_style.py`) and use it
for every slide. Consistency beats cleverness; do not restyle slide by slide.

```python
from pptx.dml.color import RGBColor
from pptx.util import Inches, Pt

W, H = 13.333, 7.5          # 16:9 in inches
MARGIN = 0.6                # left/right/top/bottom safe area
CONTENT_W = W - 2 * MARGIN

INK = RGBColor(0x1F, 0x29, 0x37)       # near-black text
MUTED = RGBColor(0x5B, 0x67, 0x78)     # captions, footnotes
ACCENT = RGBColor(0x0E, 0x74, 0x90)    # one accent, not five
ACCENT_2 = RGBColor(0xE0, 0x8A, 0x2E)  # secondary highlight
BG = RGBColor(0xFA, 0xFB, 0xFC)        # slide background
RULE = RGBColor(0xD7, 0xDD, 0xE5)      # hairlines, table borders

FONT = "Source Han Sans SC"  # or "Noto Sans / Arial"; one family, maybe two weights
TITLE = 30                  # pt
SUBTITLE = 18
BODY = 16
SMALL = 12
FOOT = 9
```

Set `presentation.slide_width = Inches(W)` and
`presentation.slide_height = Inches(H)`; use the blank layout
(`slide_layouts[6]`) and place every shape yourself.

## Layout rules

- **One message per slide.** The title is an action sentence ("7B after LoRA
  closes 80% of the gap"), not a label ("Results").
- Use a 12-column mental grid. Keep all content inside the margin; footnotes sit
  at y ≈ 7.0 in.
- Body text: 16–20 pt. Never below 12 pt for content, 9 pt for source lines.
- At most 6 bullets per slide and 2 lines per bullet. Convert prose into a
  table, chart or two-column "claim / evidence" layout instead of shrinking it.
- Leave ±0.15 in between blocks; a busy slide is a signal to split it.
- Every chart or table gets a one-line takeaway above it and a source line below.

## Slide recipes

| Purpose | Layout |
|---|---|
| Title | Large title, one-line subtitle, date/author; no bullet list |
| Section divider | Full-bleed accent band, section number + title |
| Key message | Action title + 2–4 short supporting points |
| Comparison | Two columns of equal width, same row heights |
| Evidence | Left: claim + interpretation; right: table or chart |
| Timeline / trial design | Horizontal arrows with phase blocks and visit labels |
| KPI row | 3–4 large numbers with unit + comparison period |
| Table | Header row accent-filled, zebra striping, right-aligned numbers |
| Conclusion | 3 numbered takeaways + next step |

## Tables with python-pptx

```python
def add_table(slide, rows, cols, left, top, width, height):
    shape = slide.shapes.add_table(rows, cols, Inches(left), Inches(top),
                                   Inches(width), Inches(height))
    table = shape.table
    for r in range(rows):
        table.rows[r].height = Inches(0.36)
        for c in range(cols):
            cell = table.cell(r, c)
            cell.text_frame.paragraphs[0].font.size = Pt(SMALL)
            cell.margin_left = cell.margin_right = Inches(0.08)
            cell.margin_top = cell.margin_bottom = Inches(0.03)
    return table
```

Header: `ACCENT` fill with white bold text. Body: `BG` / white zebra. Numbers
right-aligned, units in the header ("n (%)", "mg/kg"). Never paste an image of
a table the user may need to edit.

## Native charts

Use `python-pptx` charts only with real data supplied in the task. Never invent
series or draw a "typical" curve. Style for reading, not decoration:

- Remove gridlines and the chart border; keep axis labels and units.
- One colour per series from the palette; highlight the series the slide is
  about and grey the rest.
- Put data labels on the key series only; keep the legend to one row.
- Prefer horizontal bars for ranked categories, vertical columns for time
  buckets, lines for trends. Do not use 3-D or exploded pies.
- If the underlying data is unavailable, a clearly labelled schematic is better
  than fake numbers; say so in the takeaway.

## Images and icons

Use local image files in the workspace (save supplied images there). Preserve
aspect ratio, crop rather than stretch, and keep a consistent corner radius or
none at all. Do not hotlink network images: the runtime may be offline and the
deck must be self-contained. Icons: one outline family, same stroke weight, at
most two sizes.

## Accessibility

- Contrast ≥ 4.5:1 for body text, ≥ 3:1 for large text.
- Never encode meaning by red/green alone; add a label, shape or pattern.
- Order shapes the way a reader needs them: title, body, footnote.
- Alt text on images when the deck will be shared digitally.

## QA before delivery

1. Reopen the saved file with `python-pptx`; assert the expected slide count.
2. Read every text frame and compare against the source numbers and units.
3. Check overflow: library text does not auto-shrink; shorten copy or reduce
   font size rather than clipping. Re-render via the office render skill when
   appearance matters.
4. Confirm no `Click to add text`, `Lorem ipsum`, `TODO`, or duplicated slide.
5. Check the title sequence reads as a one-line story on its own.
6. Save as `<topic>.pptx` in the workspace and report the path, slide count and
   the checks performed. Mention any visual inspection that was not possible.
