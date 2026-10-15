"""Check for plan-onboarding (vague prompt). Pass: plan.md exists and is a
real plan (>= 150 words). The point of the task is whether the plan finds
the actual problem in the data, which the METRIC lines measure."""
import os, re, sys

if not os.path.exists("plan.md"):
    sys.exit("no plan.md")
t = open("plan.md", encoding="utf-8").read()
low = t.lower()
words = len(re.findall(r"\w+", t))
def near(a, b, n=200):
    return bool(re.search(rf"({a})[^\n]{{0,{n}}}({b})|({b})[^\n]{{0,{n}}}({a})", low))
m = {
    "finds_verification_drop": near(r"verif", r"drop|lose|loss|fall|only|\d+\s?%"),
    "finds_microsoft_domains": near(r"outlook|hotmail|live\.com|microsoft", r"verif|spam|junk|deliver|email"),
    "quantifies": bool(re.search(r"3[0-3](\.\d)?\s?%", t)),
    "deliverability_fix": bool(re.search(r"spf|dkim|dmarc|deliverab|sender reputation|junk|spam", low)),
    "first_priority": bool(re.search(r"(outlook|hotmail|microsoft|deliverab|verification email)", low[: max(1, len(low) // 3)])),
}
for k, v in m.items():
    print(f"METRIC {k}={int(v)}")
print(f"METRIC insights={sum(m.values())}")
print(f"METRIC words={words}")
ok = words >= 150
print("pass" if ok else f"fail: words={words}")
sys.exit(0 if ok else 1)
