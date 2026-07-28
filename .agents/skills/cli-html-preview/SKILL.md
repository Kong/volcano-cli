---
name: cli-html-preview
description: Render proposed CLI/terminal output (colors, tables, status lines, TUI reports) as a standalone HTML file and open it in the browser, so the user can review a styling/output design before it is implemented. Use whenever the user asks to "show me what the CLI output would look like", "preview the colored output", "mock up the terminal output in a browser", or wants to eyeball a color theme / table layout before code is written.
allowed-tools: Write, Bash, Read
---

# CLI output → HTML preview

Produce a throwaway HTML file that faithfully mocks what CLI output will look
like in a real terminal, then open it in the browser for review. This is a
design/preview step — it renders a *mock*, it does not run the CLI. Use it to
get sign-off on a color theme or table layout before implementing anything.

## When to use

- User wants to see a proposed color/output theme before you build it.
- Comparing "before/after" of a restyle across many subcommands at once.
- Any CLI/TUI output (tables, status lines, reports, prompts) worth eyeballing.

## Method

Generate the HTML with a small script (Python is easiest) rather than
hand-writing spans — a generator keeps column alignment perfect and scales to
many commands. Key rules that make the mock faithful:

1. **One card per subcommand.** Each output surface gets its own "terminal
   window" card (title bar with traffic-light dots + the `$ command`, then a
   `<pre>` body). This lets the user scan every command's output on one page.
2. **Monospace `<pre>`, `white-space:pre`.** Preserves exact spacing so
   columns line up. Dark background (`#010409`), light default fg.
3. **Mirror the real format widths.** If the code uses `%-20s` / fixed-width
   columns, replicate those widths in the generator so headers and rows align
   exactly as they will in the terminal. Reproduce separator rules
   (`strings.Repeat("-", N)`) at the same length.
4. **Pad THEN color.** Build the padded field first (`f"{text:<{w}}"`), then
   wrap it in a color span. In a real terminal, ANSI codes must not count
   toward column width; mocking pad-then-color keeps the preview honest about
   alignment.
5. **Palette as CSS classes.** Map each semantic color to a hex from the
   project's real palette (grep the theme source — e.g. for volcano-cli it's
   `internal/theme/theme.go`). Add a legend at the top explaining each color.
6. **Escape content** (`html.escape`) before wrapping in spans — sample data
   may contain `<`, `>`, `&`, `"`.
7. **Note the gating.** If color is TTY-gated (`NO_COLOR`/pipes/`--json` stay
   plain), say so in the page header, so the reviewer knows the mock is the
   TTY-only case.

## Generator template (adapt per project)

```python
#!/usr/bin/env python3
import html

PALETTE = {  # pull hexes from the project's real theme source
    "ok":   "#f97316",  # success / active
    "warn": "#eab308",  # warning / pending
    "err":  "#dc2626",  # error / failed
    "head": "#f37a58",  # titles / table headers
    "hint": "#f54019",  # suggested commands
    "dim":  "#6b7280",  # summaries / dim detail
}

def esc(t): return html.escape(t)
def span(text, cls=None, bold=False):
    if not cls and not bold: return esc(text)
    classes = (cls or "") + (" b" if bold else "")
    return f'<span class="{classes.strip()}">{esc(text)}</span>'
def cell(text, width, cls=None, bold=False):  # pad THEN color
    return span(f"{text:<{width}}", cls, bold)

SECTIONS = []  # (command, [lines...]) — build hdr/rows with cell() at real widths

def render():
    css = "\n".join(f".{k}{{color:{v}}}" for k, v in PALETTE.items())
    cards = "".join(
        f'<section class="card"><div class="bar">'
        f'<span class="dot r"></span><span class="dot y"></span><span class="dot g"></span>'
        f'<span class="cmd">$ {esc(cmd)}</span></div><pre>{chr(10).join(lines)}</pre></section>'
        for cmd, lines in SECTIONS)
    return f"""<!doctype html><meta charset=utf-8><style>
      body{{background:#0d1117;color:#c9d1d9;font-family:system-ui;margin:0}}
      .grid{{display:grid;grid-template-columns:repeat(auto-fill,minmax(560px,1fr));gap:18px;padding:24px}}
      .card{{background:#010409;border:1px solid #21262d;border-radius:10px;overflow:hidden}}
      .bar{{display:flex;gap:7px;align-items:center;padding:9px 12px;background:#161b22;border-bottom:1px solid #21262d}}
      .dot{{width:11px;height:11px;border-radius:50%}}
      .dot.r{{background:#ff5f56}}.dot.y{{background:#ffbd2e}}.dot.g{{background:#27c93f}}
      .cmd{{margin-left:8px;font:12px ui-monospace,Menlo,monospace;color:#8b949e}}
      pre{{margin:0;padding:16px;font:13px/1.55 ui-monospace,Menlo,monospace;color:#e6edf3;white-space:pre;overflow-x:auto}}
      .b{{font-weight:700}}
      {css}
    </style><div class="grid">{cards}</div>"""

import pathlib
out = pathlib.Path("/tmp/cli-preview/preview.html")
out.parent.mkdir(parents=True, exist_ok=True)
out.write_text(render())
print(out)
```

## Open it

```bash
python3 /tmp/cli-preview/gen.py && open /tmp/cli-preview/preview.html   # macOS
# linux: xdg-open ; wsl: wslview
```

Report the `file://` path too, in case the browser doesn't auto-launch.

## Notes

- Keep the artifact in a temp dir (`/tmp/...`); it is a preview, not a
  deliverable. Don't commit it.
- To show a before/after, render two cards per command (plain vs themed) or two
  columns.
- After sign-off, discard the mock and implement from the agreed palette/layout.
