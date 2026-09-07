#!/usr/bin/env python3
"""Convert opencode's theme assets into whip theme specs.

Usage: convert_opencode.py <opencode>/packages/tui/src/theme/assets [out dir]

Each opencode theme carries a dark and a light variant; whip gets one spec per
variant (<name>.json and <name>-light.json). Tokens map onto whip's semantic
palette, surfaces are pinned from opencode's panel/element backgrounds, and
the syntax and markdown colours are carried over. Alpha hex (#rrggbbaa) is
blended over the variant's background.
"""
import glob, json, os, re, sys

MAP = {  # whip token: opencode key
    "text": "text", "muted": "textMuted", "faint": "borderSubtle",
    "primary": "primary", "accent": "accent", "success": "success", "warning": "warning",
    "error": "error", "info": "info", "link": "markdownLinkText", "emphasis": "markdownEmph",
    "border": "border", "borderFocus": "borderActive", "bg": "background",
    "diffAdd": "diffAddedBg", "diffDel": "diffRemovedBg",
}
SYNTAX = {"keyword": "syntaxKeyword", "string": "syntaxString", "number": "syntaxNumber", "comment": "syntaxComment",
          "function": "syntaxFunction", "type": "syntaxType", "operator": "syntaxOperator", "punctuation": "syntaxPunctuation"}
MARKDOWN = {"heading": "markdownHeading", "strong": "markdownStrong", "code": "markdownCode", "quote": "markdownBlockQuote"}

def resolve(theme, defs, key, variant, depth=0):
    v = theme[key]
    if isinstance(v, dict):
        v = v.get(variant, v.get("dark"))
    if isinstance(v, str) and v in defs:
        v = defs[v]
    if isinstance(v, str) and v in theme and depth < 5:  # a reference to another token
        return resolve(theme, defs, v, variant, depth + 1)
    return v

def hexrgb(s):
    return tuple(int(s[i:i + 2], 16) for i in (1, 3, 5))

def blend(fg, bg, alpha):
    return "#%02x%02x%02x" % tuple(round(f * alpha + b * (1 - alpha)) for f, b in zip(fg, bg))

def color(v, bg):
    if not isinstance(v, str):
        return None
    if re.fullmatch(r"#[0-9a-fA-F]{8}", v):  # alpha: blend over the background
        return blend(hexrgb(v[:7]), hexrgb(bg), int(v[7:9], 16) / 255)
    if re.fullmatch(r"#[0-9a-fA-F]{6}", v):
        return v.lower()
    return None

def shift(hexcol, dark, pct):  # step a surface toward the text side
    r, g, b = hexrgb(hexcol)
    f = (lambda c: min(255, round(c + (255 - c) * pct))) if dark else (lambda c: max(0, round(c * (1 - pct))))
    return "#%02x%02x%02x" % (f(r), f(g), f(b))

def convert(path, out):
    t = json.load(open(path)); theme, defs = t["theme"], t.get("defs", {})
    base = os.path.basename(path)[:-5]
    for variant in ("dark", "light"):
        bg = color(resolve(theme, defs, "background", variant), "#000000")
        if not bg:
            continue
        pal = {}
        for tok, key in MAP.items():
            if key in theme:
                c = color(resolve(theme, defs, key, variant), bg)
                if c:
                    pal[tok] = c
        pal.setdefault("bg", bg)
        sel = theme.get("selectedListItemText")
        pal["onPrimary"] = color(resolve(theme, defs, "selectedListItemText", variant), bg) if sel else bg
        panel = color(resolve(theme, defs, "backgroundPanel", variant), bg) or shift(bg, variant == "dark", 0.04)
        element = color(resolve(theme, defs, "backgroundElement", variant), bg) or shift(panel, variant == "dark", 0.04)
        hover = shift(element, variant == "dark", 0.06)
        spec = {
            "name": base if variant == "dark" else base + "-light",
            "dark": variant == "dark",
            "palette": pal,
            "surfaces": {"panel": panel, "element": element, "hover": hover},
        }
        syn = {k: color(resolve(theme, defs, key, variant), bg) for k, key in SYNTAX.items() if key in theme}
        syn = {k: v for k, v in syn.items() if v}
        if syn:
            spec["syntax"] = syn
        md = {k: color(resolve(theme, defs, key, variant), bg) for k, key in MARKDOWN.items() if key in theme}
        md = {k: v for k, v in md.items() if v}
        if md:
            spec["markdown"] = md
        with open(os.path.join(out, spec["name"] + ".json"), "w") as f:
            json.dump(spec, f, indent=2)
            f.write("\n")

if __name__ == "__main__":
    src = sys.argv[1]; out = sys.argv[2] if len(sys.argv) > 2 else os.path.dirname(os.path.abspath(__file__))
    for p in sorted(glob.glob(os.path.join(src, "*.json"))):
        convert(p, out)
