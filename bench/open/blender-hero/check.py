"""Check for blender-hero (vague prompt). Pass: renders/hero.png exists, is a
real image (not blank) and comes from the saved scene. METRIC lines describe
it; quality is for the judge and the human rating."""
import os, sys
sys.path.insert(0, os.path.join(os.environ["TASK_DIR"], "..", "_lib"))
from blender_check import inspect

d = inspect("renders/hero.png")
img = d["images"].get("renders/hero.png") or {}
errs = []
if "width" not in img:
    errs.append("no renders/hero.png")
elif img["std"] < 0.03:
    errs.append(f"blank image {img}")
meshes = [o for o in d["objects"] if o["type"] == "MESH" and not o["hide_render"]]
if not meshes:
    errs.append("no meshes in scene.blend")
w, h = img.get("width", 0), img.get("height", 1)
print(f"METRIC width={w}")
print(f"METRIC aspect={round(w / h, 2) if h else 0}")
print(f"METRIC meshes={len(meshes)}")
print(f"METRIC lights={sum(1 for o in d['objects'] if o['type'] == 'LIGHT')}")
print(f"METRIC brand_material={int(any(any(m.get('principled') for m in o.get('materials') or []) for o in meshes))}")
print("pass" if not errs else "fail: " + "; ".join(errs))
sys.exit(0 if not errs else 1)
