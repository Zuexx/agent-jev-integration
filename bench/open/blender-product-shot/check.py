"""Check for blender-product-shot. Pass: renders/shot.png is a 640x360
non-blank render; the scene has Mug and Table meshes with materials, at least
three lights, a 50 mm active camera, and the mug well inside the frame."""
import os, sys
sys.path.insert(0, os.path.join(os.environ["TASK_DIR"], "..", "_lib"))
from blender_check import inspect, obj

d = inspect("renders/shot.png")
errs = []
img = d["images"].get("renders/shot.png")
if not img or img.get("width") != 640 or img.get("height") != 360:
    errs.append(f"render {img}")
elif img["std"] < 0.02:
    errs.append("render is blank")
mug, table = obj(d, "Mug"), obj(d, "Table")
if not mug or mug["type"] != "MESH" or not table or table["type"] != "MESH":
    errs.append("Mug/Table meshes missing")
lights = [o for o in d["objects"] if o["type"] == "LIGHT" and o["light"]["energy"] > 0 and not o["hide_render"]]
if len(lights) < 3:
    errs.append(f"{len(lights)} lights")
cam = obj(d, d["camera"]) if d["camera"] else None
if not cam or abs(cam.get("lens_mm", 0) - 50) > 0.5:
    errs.append("camera lens")
v = (mug or {}).get("view") or {}
if v.get("inside", 0) < 0.75 or not (0.02 <= v.get("area", 0) <= 0.85):
    errs.append(f"mug framing {v}")
mats = lambda o: (o or {}).get("materials") or []
if not mats(mug) or not mats(table):
    errs.append("materials missing")
pm = (mats(mug)[0].get("principled") if mats(mug) else None) or {}
pt = (mats(table)[0].get("principled") if mats(table) else None) or {}
tc = pt.get("base_color", [0, 0, 0])
plaus = {
    "mug_not_metal": pm.get("metallic", 1) < 0.3,
    "table_wood_tone": tc[0] > tc[2] and tc[0] >= tc[1] >= tc[2] * 0.8 and tc[0] > 0.05,
    "handle": sum(1 for o in d["objects"] if o["type"] == "MESH" and (o["parent"] or "").lower() == "mug") > 0 or (mug or {}).get("vertices", 0) > 600,
}
for k, val in plaus.items():
    print(f"METRIC {k}={int(val)}")
print(f"METRIC mug_area={v.get('area', 0)}")
print(f"METRIC lights={len(lights)}")
print("pass" if not errs else "fail: " + "; ".join(errs))
sys.exit(0 if not errs else 1)
