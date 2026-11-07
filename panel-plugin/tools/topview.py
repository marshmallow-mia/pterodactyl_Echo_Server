"""Turns a cleaned drawing into a view from above. The drawings put z (the game's
depth) to the right and x down, the way /session's numbers read; with y up that
is the level seen from below. Mirroring x (x up) makes it a top view: the
drawing goes into <g transform="scale(1,-1)">, its texts stay readable outside
it at -y, and the viewBox's vertical range is negated. The map's dots use the
same -x (Format::mapPoint, echovrLiveMap)."""
import xml.etree.ElementTree as ET

NS = "http://www.w3.org/2000/svg"


def top_view(root, view):
    min_z, min_x, w, h = view
    texts = [e for e in root if e.tag.split("}")[1] == "text"]
    g = ET.Element("{%s}g" % NS, {"transform": "scale(1,-1)"})
    for e in list(root):
        root.remove(e)
        if e not in texts:
            g.append(e)
    root.append(g)
    for t in texts:
        y = -float(t.get("y", "0"))
        t.set("y", ("%g" % y) if y != 0 else "0")
        root.append(t)
    root.set("viewBox", " ".join("%g" % v for v in (min_z, -(min_x + h), w, h)))
