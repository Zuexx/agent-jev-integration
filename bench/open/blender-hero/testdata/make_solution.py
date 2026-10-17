"""Reference hero (deliberately simple): a notebook and pencil in brand
colours, wide 1280x720. blender -b --factory-startup --python make_solution.py"""
import bpy, math, os
here = os.path.dirname(os.path.abspath(__file__)); out = os.path.join(here, "solution")
os.makedirs(os.path.join(out, "renders"), exist_ok=True)
bpy.ops.wm.read_factory_settings(use_empty=True)
def hexrgb(h): return tuple((int(h[i:i + 2], 16) / 255) ** 2.2 for i in (1, 3, 5))
def mat(n, h, r=0.5):
    m = bpy.data.materials.new(n); m.use_nodes = True; b = m.node_tree.nodes["Principled BSDF"]
    b.inputs["Base Color"].default_value = (*hexrgb(h), 1); b.inputs["Roughness"].default_value = r; return m
bpy.ops.mesh.primitive_plane_add(size=30); bpy.context.object.data.materials.append(mat("Desk", "#1F3A5F", 0.8))
bpy.ops.mesh.primitive_cube_add(location=(0, 0, 0.08)); nb = bpy.context.object; nb.name = "Notebook"; nb.scale = (1.6, 1.1, 0.08)
nb.rotation_euler = (0, 0, math.radians(-12)); nb.data.materials.append(mat("Paper", "#F7F4EC", 0.9))
bpy.ops.mesh.primitive_cylinder_add(radius=0.06, depth=2.2, location=(1.9, 0.2, 0.08), rotation=(0, math.radians(90), math.radians(25)))
p = bpy.context.object; p.name = "Pencil"; p.data.materials.append(mat("Accent", "#E8A33D", 0.4))
bpy.ops.object.light_add(type="AREA", location=(-3, -3, 5)); l = bpy.context.object; l.data.energy = 900; l.data.size = 4
l.rotation_euler = (math.radians(35), 0, math.radians(-45))
bpy.ops.object.camera_add(location=(0.6, -5.5, 4.2), rotation=(math.radians(50), 0, math.radians(4))); cam = bpy.context.object
sc = bpy.context.scene; sc.camera = cam
sc.render.resolution_x, sc.render.resolution_y, sc.render.resolution_percentage = 1280, 720, 100
sc.render.engine = "CYCLES"; sc.cycles.device = "CPU"; sc.cycles.samples = 48
sc.render.filepath = os.path.join(out, "renders", "hero.png"); bpy.ops.render.render(write_still=True)
bpy.ops.wm.save_as_mainfile(filepath=os.path.join(out, "scene.blend"))
