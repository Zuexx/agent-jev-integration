"""Check for plan-ci-migration. Pass: plan.md has milestone, dependency and
risk sections and names at least two of the five hidden constraints. METRIC
lines count every constraint the plan handles."""
import os, re, sys

sys.path.insert(0, os.path.join(os.environ["TASK_DIR"], "..", "_lib"))
from find_output import find_output

path = find_output("plan.md")
if not path:
    sys.exit("no plan.md")
t = open(path, encoding="utf-8").read()
low = t.lower()
heads = " ".join(re.findall(r"^#+\s*(.+)$", t, re.M)).lower()
sections = all(k in heads for k in ("milestone", "risk", "depend"))
risks = {
    "long_jobs": bool(re.search(r"6[- ]?h|six hours|6 hours|360|540|420|ml-train|data-backfill", low)) and "self-hosted" in low,
    # Naming the requirement is not enough: the plan has to close the gap
    # between the 90-day default and the one-year rule.
    "log_retention": bool(re.search(r"(retention|retain|90 days)[^\n]{0,200}(400|raise|increase|extend|archiv|export|copy|ship)|(400|raise|increase|extend|archiv|export)[^\n]{0,200}(retention|logs)", low)),
    "freeze": bool(re.search(r"freeze", low)),
    "gpu": "gpu" in low,
    "vault": "vault" in low,
}
for k, v in risks.items():
    print(f"METRIC risk_{k}={int(v)}")
print(f"METRIC risks={sum(risks.values())}")
dated = len(set(re.findall(r"20\d\d-\d\d-\d\d", t)))
print(f"METRIC dates={dated}")
ok = sections and sum(risks.values()) >= 2
print("pass" if ok else f"fail: sections={sections} risks={sum(risks.values())}")
sys.exit(0 if ok else 1)
