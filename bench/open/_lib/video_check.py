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

def black_seconds(path, min_len=0.5, pix_th=0.03):
    """Seconds of (near) pure black in stretches of min_len or more; pix_th is
    low so dark themes (terminal-style navy backgrounds) do not count."""
    p = subprocess.run(["ffmpeg", "-hide_banner", "-i", path, "-vf", f"blackdetect=d={min_len}:pix_th={pix_th}", "-an", "-f", "null", "-"],
                       capture_output=True, text=True)
    return round(sum(float(x) for x in re.findall(r"black_duration:([\d.]+)", p.stderr)), 2)

def distinct_frames(path, samples=10, threshold=0.015):
    """How many visibly different frames a video has: samples evenly spaced
    frames at 160x90 grey and keeps those that differ from every kept frame
    by more than threshold (mean absolute difference, 0-1). Motion graphics
    often keep one background and change only text, so frame-to-frame scene
    detection and coarse thumbnails both miss their scenes; this does not."""
    dur = probe(path)["duration"]
    w, h = 160, 90
    kept = []
    for i in range(samples):
        t = dur * (i + 0.5) / samples
        p = subprocess.run(["ffmpeg", "-v", "error", "-ss", f"{t:.2f}", "-i", path, "-frames:v", "1",
                            "-vf", f"scale={w}:{h},format=gray", "-f", "rawvideo", "-"], capture_output=True)
        f = p.stdout[: w * h]
        if len(f) < w * h:
            continue
        if all(sum(abs(a - b) for a, b in zip(f, k)) / (w * h * 255) > threshold for k in kept):
            kept.append(f)
    return len(kept)
