# Usage: python3 -I tools/clean_lobby.py lobby-with-sections.svg echovr-match/resources/maps/mpl_lobby_b2.svg
"""Cleans lobby-with-sections.svg for the live map: cropped to the lobby frame (z -85..25, x -65..85, meters),
no background, axis numbers, height labels or legend; the two section names stay; coordinates rounded; seen
from above (topview.py: x mirrored, viewBox -85 -85 110 150)."""
import os, re, sys, xml.etree.ElementTree as ET
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from topview import top_view
NS = "http://www.w3.org/2000/svg"
ET.register_namespace("", NS)
src, dst = sys.argv[1], sys.argv[2]
root = ET.parse(src).getroot()
VIEW = (-85, -65, 110, 150)
def tag(e): return e.tag.split("}")[1]
drop = []
for e in list(root):
    t = tag(e)
    if t == "text":
        if (e.text or "").strip() not in ("arena section", "combat section"):
            drop.append(e)
    elif t in ("rect", "circle"):
        y = float(e.get("y", e.get("cy", "0")))
        if y > 85 or (t == "rect" and e.get("fill") == "#16161a"):
            drop.append(e)
for e in drop: root.remove(e)
def fmt(m):
    v = ("%.2f" % float(m.group(0))).rstrip("0").rstrip(".")
    return "0" if v in ("-0", "") else v
num = re.compile(r"-?\d+\.\d+(?:e-?\d+)?")
for e in root.iter():
    for k, v in list(e.attrib.items()):
        if k in ("d", "points", "x", "y", "x1", "x2", "y1", "y2", "cx", "cy", "r", "width", "height", "stroke-width", "font-size"):
            e.set(k, num.sub(fmt, v))
top_view(root, VIEW)
root.set("preserveAspectRatio", "xMidYMid meet")
for k in ("width", "height"): root.attrib.pop(k, None)
out = ET.tostring(root, encoding="unicode").replace("ns0:", "").replace(":ns0", "")
open(dst, "w").write("<!-- Echo VR lobby (mpl_lobby_b2) with its arena and combat sections, seen from above (x mirrored), meters; map by Spritz Metis Sprock -->\n" + out + "\n")
print("dropped", len(drop))
