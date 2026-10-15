"""Builds fixture/scene.blend: a robot that renders black for three reasons
(the robot is excluded from renders, the only light has zero energy, the
camera faces away). With --solution it builds the fixed scene and its render
in testdata/solution instead."""
import bpy, math, os, sys
here = os.path.dirname(os.path.abspath(__file__))
fixed = "--solution" in sys.argv
bpy.ops.wm.read_factory_settings(use_empty=True)
m = bpy.data.materials.new("Paint"); m.use_nodes = True
m.node_tree.nodes["Principled BSDF"].inputs["Base Color"].default_value = (0.8, 0.3, 0.1, 1)
parts = []
for loc, scale in [((0, 0, 1), (0.6, 0.4, 0.8)), ((0, 0, 2.1), (0.35, 0.35, 0.35)), ((0.75, 0, 1.1), (0.15, 0.15, 0.6)),
                   ((-0.75, 0, 1.1), (0.15, 0.15, 0.6)), ((0.25, 0, 0.2), (0.15, 0.15, 0.4)), ((-0.25, 0, 0.2), (0.15, 0.15, 0.4))]:
    bpy.ops.mesh.primitive_cube_add(location=loc); o = bpy.context.object; o.scale = scale; o.data.materials.append(m); parts.append(o)
bpy.ops.object.select_all(action="DESELECT")
for o in parts: o.select_set(True)
bpy.context.view_layer.objects.active = parts[0]; bpy.ops.object.join()
robot = bpy.context.object; robot.name = "Robot"
bpy.ops.mesh.primitive_plane_add(size=12); bpy.context.object.name = "Floor"
bpy.ops.object.light_add(type="SUN", location=(3, -3, 6)); sun = bpy.context.object; sun.name = "Sun"
sun.rotation_euler = (math.radians(40), 0, math.radians(30))
bpy.ops.object.camera_add(location=(0, -7, 2.2)); cam = bpy.context.object; cam.name = "Camera"
sc = bpy.context.scene; sc.camera = cam
sc.render.resolution_x, sc.render.resolution_y, sc.render.resolution_percentage = 480, 270, 100
if fixed:
    sun.data.energy = 4
    cam.location = (0, -10, 1.4); cam.rotation_euler = (math.radians(88), 0, 0)
    out = os.path.join(here, "solution")
    os.makedirs(os.path.join(out, "renders"), exist_ok=True)
    sc.render.engine = "CYCLES"; sc.cycles.device = "CPU"; sc.cycles.samples = 32
    sc.render.filepath = os.path.join(out, "renders", "fixed.png"); bpy.ops.render.render(write_still=True)
    bpy.ops.wm.save_as_mainfile(filepath=os.path.join(out, "scene.blend"))
else:
    robot.hide_render = True
    sun.data.energy = 0
    cam.rotation_euler = (math.radians(85), 0, math.radians(180))
    bpy.ops.wm.save_as_mainfile(filepath=os.path.join(here, "..", "fixture", "scene.blend"))
