"""Run inside Blender: blender -b scene.blend --python blender_inspect.py -- [image.png ...]
Prints one JSON line after the marker: the scene's objects, materials,
lights, camera, how much of each mesh the active camera sees, and stats of
the given images. Used by the Blender tasks' checks."""
import bpy, json, sys
from bpy_extras.object_utils import world_to_camera_view
from mathutils import Vector

sc = bpy.context.scene
cam = sc.camera

def principled(mat):
    if not mat or not mat.use_nodes:
        return None
    for n in mat.node_tree.nodes:
        if n.type == "BSDF_PRINCIPLED":
            c = n.inputs["Base Color"].default_value
            return {"base_color": [round(c[0], 3), round(c[1], 3), round(c[2], 3)],
                    "metallic": round(n.inputs["Metallic"].default_value, 3),
                    "roughness": round(n.inputs["Roughness"].default_value, 3)}
    return None

def view(o):
    """Fraction of bounding-box corners inside the frame and the projected
    bounding-box area as a fraction of the frame."""
    if not cam:
        return None
    pts = [world_to_camera_view(sc, cam, o.matrix_world @ Vector(c)) for c in o.bound_box]
    inside = sum(1 for p in pts if 0 <= p.x <= 1 and 0 <= p.y <= 1 and p.z > 0)
    xs = [min(max(p.x, 0), 1) for p in pts if p.z > 0]
    ys = [min(max(p.y, 0), 1) for p in pts if p.z > 0]
    area = (max(xs) - min(xs)) * (max(ys) - min(ys)) if xs else 0
    return {"inside": inside / 8, "area": round(area, 4), "in_front": all(p.z > 0 for p in pts)}

objs = []
for o in sc.objects:
    d = {"name": o.name, "type": o.type, "parent": o.parent.name if o.parent else None,
         "hide_render": o.hide_render, "location": [round(v, 3) for v in o.location],
         "dimensions": [round(v, 3) for v in o.dimensions]}
    if o.type == "MESH":
        d["materials"] = [{"name": s.material.name, "principled": principled(s.material)} for s in o.material_slots if s.material]
        d["vertices"] = len(o.data.vertices)
        d["view"] = view(o)
    if o.type == "LIGHT":
        d["light"] = {"type": o.data.type, "energy": o.data.energy}
    if o.type == "CAMERA":
        d["lens_mm"] = round(o.data.lens, 2)
    objs.append(d)

images = {}
args = sys.argv[sys.argv.index("--") + 1:] if "--" in sys.argv else []
for path in args:
    try:
        img = bpy.data.images.load(path)
        w, h = img.size
        import numpy as np
        px = np.empty(w * h * 4, dtype=np.float32)
        img.pixels.foreach_get(px)
        px = px.reshape(-1, 4)
        lum = 0.2126 * px[:, 0] + 0.7152 * px[:, 1] + 0.0722 * px[:, 2]
        mean, std = float(lum.mean()), float(lum.std())
        images[path] = {"width": w, "height": h, "mean": round(mean, 4), "std": round(std, 4)}
    except Exception as e:
        images[path] = {"error": str(e)}

out = {"objects": objs, "camera": cam.name if cam else None,
       "resolution": [sc.render.resolution_x * sc.render.resolution_percentage // 100,
                      sc.render.resolution_y * sc.render.resolution_percentage // 100],
       "world_strength": None, "images": images}
if sc.world and sc.world.use_nodes:
    for n in sc.world.node_tree.nodes:
        if n.type == "BACKGROUND":
            out["world_strength"] = n.inputs["Strength"].default_value
print("<<<INSPECT>>>" + json.dumps(out))
