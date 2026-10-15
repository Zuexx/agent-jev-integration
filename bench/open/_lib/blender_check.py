"""Shared helper for the Blender tasks' check.py: runs blender_inspect.py
on scene.blend (plus images) and returns the parsed result."""
import json, os, subprocess, sys

def inspect(*images):
    blender = os.environ.get("BLENDER")
    if not blender:
        sys.exit("BLENDER is not set")
    if not os.path.exists("scene.blend"):
        sys.exit("no scene.blend")
    here = os.path.dirname(os.path.abspath(__file__))
    imgs = [os.path.abspath(i) for i in images if os.path.exists(i)]
    p = subprocess.run([blender, "-b", "--factory-startup", "scene.blend", "--python", os.path.join(here, "blender_inspect.py"), "--", *imgs],
                       capture_output=True, text=True, timeout=300)
    for line in p.stdout.splitlines():
        if line.startswith("<<<INSPECT>>>"):
            data = json.loads(line[len("<<<INSPECT>>>"):])
            data["images"] = {os.path.relpath(k): v for k, v in data["images"].items()}
            return data
    sys.exit("inspect failed: " + p.stdout[-500:] + p.stderr[-500:])

def obj(data, name):
    for o in data["objects"]:
        if o["name"].lower() == name.lower():
            return o
    return None
