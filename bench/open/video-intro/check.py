"""Check for video-intro. Pass: out/intro.mp4 is 1280x720 at 30 fps, 19.5-20.5 s
long, has no black stretch of 0.5 s or more, at least 4 scene changes (title,
three features, end card), and the source names the three features and the URL."""
import glob, os, sys
sys.path.insert(0, os.path.join(os.environ["TASK_DIR"], "..", "_lib"))
from video_check import probe, black_seconds, scene_changes

p = "out/intro.mp4"
if not os.path.exists(p):
    sys.exit("no " + p)
m = probe(p) or {}
errs = []
if (m.get("width"), m.get("height"), m.get("fps")) != (1280, 720, 30):
    errs.append(f"format {m}")
if not 19.5 <= m.get("duration", 0) <= 20.5:
    errs.append(f"duration {m.get('duration')}")
black = black_seconds(p)
scenes = scene_changes(p)
if black > 0:
    errs.append(f"{black}s black")
if scenes < 4:
    errs.append(f"{scenes} scene changes")
src = " ".join(open(f, encoding="utf-8").read().lower() for f in glob.glob("src/**/*.tsx", recursive=True) + glob.glob("src/**/*.ts", recursive=True))
missing = [w for w in ("scan", "plan", "undo", "tidy.dev") if w not in src]
if missing:
    errs.append(f"source lacks {missing}")
print(f"METRIC black_seconds={black}")
print(f"METRIC scene_changes={scenes}")
print("pass" if not errs else "fail: " + "; ".join(errs))
sys.exit(0 if not errs else 1)
