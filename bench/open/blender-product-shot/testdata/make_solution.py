"""Builds the reference solution: blender -b --factory-startup --python make_solution.py
Writes testdata/solution/scene.blend and testdata/solution/renders/shot.png."""
import bpy, math, os
here = os.path.dirname(os.path.abspath(__file__))
out = os.path.join(here, "solution")
os.makedirs(os.path.join(out, "renders"), exist_ok=True)
bpy.ops.wm.read_factory_settings(use_empty=True)

def mat(name, color, rough, metal=0.0):
    m = bpy.data.materials.new(name); m.use_nodes = True
    b = m.node_tree.nodes["Principled BSDF"]
    b.inputs["Base Color"].default_value = (*color, 1); b.inputs["Roughness"].default_value = rough; b.inputs["Metallic"].default_value = metal
    return m

bpy.ops.mesh.primitive_cube_add(size=1, location=(0, 0, -0.05)); table = bpy.context.object
table.name = "Table"; table.scale = (3, 2, 0.1); table.data.materials.append(mat("Wood", (0.35, 0.2, 0.09), 0.6))
bpy.ops.mesh.primitive_cylinder_add(vertices=64, radius=0.4, depth=0.9, location=(0, 0, 0.45)); mug = bpy.context.object
mug.name = "Mug"; ceramic = mat("Ceramic", (0.9, 0.9, 0.88), 0.25); mug.data.materials.append(ceramic)
bpy.ops.mesh.primitive_torus_add(major_radius=0.22, minor_radius=0.05, location=(0.45, 0, 0.5), rotation=(math.pi / 2, 0, 0))
handle = bpy.context.object; handle.name = "Handle"; handle.data.materials.append(ceramic); handle.parent = mug
handle.matrix_parent_inverse = mug.matrix_world.inverted()
for name, loc, energy, size in [("Key", (2.5, -2.5, 3), 400, 1.5), ("Fill", (-3, -2, 2), 120, 3), ("Rim", (0, 3, 2.5), 300, 1)]:
    bpy.ops.object.light_add(type="AREA", location=loc); l = bpy.context.object; l.name = name
    l.data.energy = energy; l.data.size = size
    l.rotation_euler = (mug.location - l.location).to_track_quat("-Z", "Y").to_euler()
bpy.ops.object.camera_add(location=(0, -4.2, 1.6)); cam = bpy.context.object; cam.data.lens = 50
cam.rotation_euler = ((mug.location + mug.location * 0) - cam.location).to_track_quat("-Z", "Y").to_euler()
sc = bpy.context.scene; sc.camera = cam
sc.render.resolution_x, sc.render.resolution_y, sc.render.resolution_percentage = 640, 360, 100
sc.render.engine = "CYCLES"; sc.cycles.device = "CPU"; sc.cycles.samples = 64
sc.render.filepath = os.path.join(out, "renders", "shot.png"); bpy.ops.render.render(write_still=True)
bpy.ops.wm.save_as_mainfile(filepath=os.path.join(out, "scene.blend"))
