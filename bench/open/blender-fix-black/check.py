"""Check for blender-fix-black. Pass: renders/fixed.png is not black and the
Robot is rendered and in front of the active camera. METRIC lines say which of
the three planted causes were fixed."""
import os, sys
sys.path.insert(0, os.path.join(os.environ["TASK_DIR"], "..", "_lib"))
from blender_check import inspect, obj

d = inspect("renders/fixed.png")
errs = []
img = d["images"].get("renders/fixed.png")
if not img or "mean" not in img:
    errs.append("no renders/fixed.png")
elif img["mean"] < 0.04 or img["std"] < 0.02:
    errs.append(f"render still dark or flat {img}")
robot = obj(d, "Robot")
if not robot:
    errs.append("Robot missing")
else:
    v = robot.get("view") or {}
    if robot["hide_render"] or v.get("inside", 0) < 0.75 or not v.get("in_front"):
        errs.append(f"Robot not visible: hide_render={robot['hide_render']} view={v}")
lit = any(o["type"] == "LIGHT" and o["light"]["energy"] > 0 and not o["hide_render"] for o in d["objects"]) or (d.get("world_strength") or 0) > 0
fixes = {"robot_renderable": bool(robot) and not robot["hide_render"], "light_on": lit,
         "camera_faces_robot": bool(robot) and bool((robot.get("view") or {}).get("in_front"))}
for k, val in fixes.items():
    print(f"METRIC fixed_{k}={int(val)}")
print(f"METRIC fixed={sum(fixes.values())}")
print("pass" if not errs else "fail: " + "; ".join(errs))
sys.exit(0 if not errs else 1)
