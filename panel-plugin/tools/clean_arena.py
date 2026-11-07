# Usage: python3 -I tools/clean_arena.py arena-topdown-v3.svg echovr-match/resources/maps/mpl_arena_a.svg
"""Cleans arena-topdown-v3.svg for the live map: no text, no label boxes, no
title or legend, cropped to the arena frame (z -80..80, x -18..18, meters),
goal colours as in /session (blue defends -z), coordinates rounded, seen from
above (topview.py: x mirrored, viewBox -80 -18 160 36)."""
import os, re, sys, xml.etree.ElementTree as ET

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from topview import top_view

NS = "http://www.w3.org/2000/svg"
ET.register_namespace("", NS)
BLUE, ORANGE = "#4a7ee8", "#e8792f"
src, dst = sys.argv[1], sys.argv[2]
root = ET.parse(src).getroot()

def tag(e): return e.tag.split("}")[1]
def nums(s): return [float(x) for x in re.findall(r"-?\d+(?:\.\d+)?(?:e-?\d+)?", s)]
def yrange(e):
    t = tag(e)
    if t == "rect": y = float(e.get("y")); return y, y + float(e.get("height"))
    if t == "line": a, b = float(e.get("y1")), float(e.get("y2")); return min(a, b), max(a, b)
    if t == "circle": c, r = float(e.get("cy")), float(e.get("r")); return c - r, c + r
    if t == "path": yy = nums(e.get("d"))[1::2]; return (min(yy), max(yy)) if yy else None
    return None

removed = {"text": 0, "label box": 0, "outside": 0, "background": 0}
for e in list(root):
    t = tag(e)
    if t == "text":
        removed["text"] += 1; root.remove(e); continue
    if t == "rect" and e.get("rx") is not None and e.get("fill-opacity") is not None:
        removed["label box"] += 1; root.remove(e); continue
    if t == "rect" and e.get("x") == "-80" and e.get("y") == "-22.0":
        removed["background"] += 1; root.remove(e); continue
    y = yrange(e)
    if y and (y[0] >= 18 or y[1] <= -18):
        removed["outside"] += 1; root.remove(e); continue
    # Goal colours: /session puts blue's spawn and goal at -z, orange's at +z.
    for a in ("fill", "stroke"):
        v = (e.get(a) or "").lower()
        if v in (BLUE, ORANGE):
            xs = nums(e.get("d"))[0::2] if t == "path" else [float(e.get("x1") or e.get("x") or 0)]
            e.set(a, BLUE if sum(xs) / len(xs) < 0 else ORANGE)
    for a in ("d", "x", "y", "x1", "y1", "x2", "y2", "cx", "cy", "r", "width", "height"):
        if e.get(a):
            e.set(a, re.sub(r"-?\d+\.\d+(?:e-?\d+)?", lambda m: f"{float(m.group()):.2f}".rstrip("0").rstrip("."), e.get(a)))

root.set("viewBox", "-80 -18 160 36")
top_view(root, (-80, -18, 160, 36))
for a in ("width", "height"):
    root.attrib.pop(a, None)
root.set("preserveAspectRatio", "xMidYMid meet")
ET.ElementTree(root).write(dst, encoding="unicode", short_empty_elements=True)
left = {}
for e in root:
    left[tag(e)] = left.get(tag(e), 0) + 1
print("removed", removed, "kept", left)
