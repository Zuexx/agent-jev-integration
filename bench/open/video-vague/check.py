"""Check for video-vague. Pass: one rendered video in out/, 5-120 s, at most
30% black, at least two visibly different frames. Quality is for the judge and the
human rating; METRIC lines describe the video."""
import glob, os, sys
sys.path.insert(0, os.path.join(os.environ["TASK_DIR"], "..", "_lib"))
from video_check import probe, black_seconds, distinct_frames

vids = [v for v in glob.glob("out/*") if v.lower().endswith((".mp4", ".mov", ".webm"))]
if not vids:
    sys.exit("no video in out/")
p = max(vids, key=os.path.getmtime)
m = probe(p) or {}
dur = m.get("duration", 0)
black, scenes = black_seconds(p), distinct_frames(p)
errs = []
if not 5 <= dur <= 120:
    errs.append(f"duration {dur}")
if dur and black / dur > 0.3:
    errs.append(f"{black}s black")
if scenes < 2:
    errs.append(f"only {scenes} distinct frames")
for k in ("width", "height", "fps"):
    print(f"METRIC {k}={m.get(k, 0)}")
print(f"METRIC duration={round(dur, 2)}")
print(f"METRIC black_seconds={black}")
print(f"METRIC distinct_frames={scenes}")
print("pass" if not errs else "fail: " + "; ".join(errs))
sys.exit(0 if not errs else 1)
