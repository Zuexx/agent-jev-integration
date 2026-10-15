"""Check for design-ratelimit. Pass: the design doc exists, has the main
sections, and recognises that an exact cross-region limit cannot meet the
2 ms budget and partition rule together. METRIC lines grade the rest."""
import os, re, sys

p = "docs/design/rate-limit.md"
if not os.path.exists(p):
    sys.exit("no " + p)
t = open(p, encoding="utf-8").read()
low = t.lower()
heads = " ".join(re.findall(r"^#+\s*(.+)$", t, re.M)).lower()
sections = sum(k in heads for k in ("context", "option", "decision", "trade", "failure")) >= 4
def near(a, b, n=220):
    return bool(re.search(rf"({a})[^\n]{{0,{n}}}({b})|({b})[^\n]{{0,{n}}}({a})", low))
conflict = near(r"global|across (all )?regions|cross-region|exact", r"approximat|eventual|overshoot|over-admit|cannot|can't|impossible|not possible|conflict|local (quota|budget|share)")
m = {
    "uses_rtt": bool(re.search(r"\b(70|160|180)\s?ms", low)),
    "separates_billing": near(r"billing|finance|invoice|1\s?%", r"async|separate|reconcil|aggregat|batch|offline|exact|log"),
    "partition_behaviour": near(r"partition", r"fail[- ]?open|keep serving|local|own share|fallback|degrad"),
    "bounds_abuse": near(r"3\s?x|three times|abuse|overshoot|over-admit", r"bound|at most|worst|limit|cap"),
}
for k, v in m.items():
    print(f"METRIC {k}={int(v)}")
print(f"METRIC points={sum(m.values())}")
ok = sections and conflict
print("pass" if ok else f"fail: sections={sections} conflict={conflict}")
sys.exit(0 if ok else 1)
