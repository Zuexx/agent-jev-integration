"""Shared helpers for the video tasks' checks: ffprobe metadata, black
segments and scene changes via ffmpeg."""
import json, re, subprocess

def probe(path):
    p = subprocess.run(["ffprobe", "-v", "error", "-select_streams", "v:0", "-show_entries",
                        "stream=width,height,r_frame_rate:format=duration", "-of", "json", path], capture_output=True, text=True)
    if p.returncode != 0:
        return None
    d = json.loads(p.stdout)
    s = d["streams"][0]
    num, den = s["r_frame_rate"].split("/")
    return {"width": s["width"], "height": s["height"], "fps": round(int(num) / int(den), 2), "duration": float(d["format"]["duration"])}

def black_seconds(path, min_len=0.5):
    p = subprocess.run(["ffmpeg", "-hide_banner", "-i", path, "-vf", f"blackdetect=d={min_len}:pix_th=0.10", "-an", "-f", "null", "-"],
                       capture_output=True, text=True)
    return round(sum(float(x) for x in re.findall(r"black_duration:([\d.]+)", p.stderr)), 2)

def scene_changes(path, threshold=0.1):
    p = subprocess.run(["ffmpeg", "-hide_banner", "-i", path, "-vf", f"select='gt(scene,{threshold})',showinfo", "-an", "-f", "null", "-"],
                       capture_output=True, text=True)
    return len(re.findall(r"pts_time:", p.stderr))
