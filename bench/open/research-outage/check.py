"""Check for research-outage. Pass: findings.md names the expired TLS
certificate as the cause and pins the start at 09:00. METRIC lines grade
whether it refutes the draft's deploy theory, dismisses the DB spike, names
the missed expiry warnings, and gives the recovery time."""
import os, re, sys

if not os.path.exists("findings.md"):
    sys.exit("no findings.md")
t = open("findings.md", encoding="utf-8").read()
low = t.lower()
cert = bool(re.search(r"(certificate|cert\b|x509|tls)", low)) and bool(re.search(r"expir", low))
start = "09:00" in t
def near(a, b, n=160):
    return bool(re.search(rf"({a})[^\n]{{0,{n}}}({b})|({b})[^\n]{{0,{n}}}({a})", low))
metrics = {
    "refutes_deploy": near(r"v4\.12|deploy|rollback", r"\bnot\b|n't|after|unrelated|coincid|no effect|did not|before the deploy|already"),
    "dismisses_db": near(r"\bdb\b|database|cpu", r"\bnot\b|n't|unrelated|resolved|normal|before|red herring|coincid"),
    "missed_warnings": near(r"warn|alert|monitor|notif|days_left|#platform-alerts", r"ignor|miss|unnoticed|no one|nobody|not acted|went unanswered|7 days|three days|3 days|seven days"),
    "recovery_time": "10:05" in t or "10:06" in t or "10:04" in t,
}
for k, v in metrics.items():
    print(f"METRIC {k}={int(v)}")
print(f"METRIC insights={sum(metrics.values())}")
ok = cert and start
print("pass" if ok else f"fail: cert_cause={cert} start_0900={start}")
sys.exit(0 if ok else 1)
