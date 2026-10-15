"""Check for research-mq. Pass: recommends Orbitline, at least 4 citations,
and every quote appears verbatim in the cited corpus file (no fabrication).
METRIC lines grade how many of the corpus's four traps the answer names."""
import os, re, sys

def norm(s):
    return re.sub(r"\s+", " ", s.replace("’", "'").replace("**", "")).strip().lower()

if not os.path.exists("answer.md"):
    sys.exit("no answer.md")
text = open("answer.md", encoding="utf-8").read()
corpus = {f: norm(open(os.path.join("corpus", f), encoding="utf-8").read()) for f in os.listdir("corpus")}

rec = re.search(r"^\s*\**Recommendation\**\s*:\s*(.+)$", text, re.M | re.I)
rec_ok = bool(rec) and "orbitline" in rec.group(1).lower() and not re.search(r"kestrel|pipewing", rec.group(1), re.I)

good = bad = 0
for m in re.finditer(r'^\s*>\s*["“](.+?)["”]\s*[—–-]+\s*\[([^\]]+)\]', text, re.M):
    quote, src = m.group(1), m.group(2).strip()
    body = corpus.get(src) or corpus.get(os.path.basename(src))
    # "..." may join pieces of one passage; each piece must appear, in order.
    pieces = [norm(p) for p in re.split(r"\.\.\.|…", quote) if norm(p)]
    pos, found = 0, body is not None
    for p in pieces if found else []:
        i = body.find(p, pos)
        if i < 0:
            found = False
            break
        pos = i + len(p)
    if found and pieces:
        good += 1
    else:
        bad += 1
        print("not in corpus:", src, "|", quote[:100])

low = text.lower()
traps = {
    "pricing_update": "0.05" in low,
    "benchmark_partitions": "partition" in low,
    "pipewing_at_least_once": "at-least-once" in low or "at least once" in low,
    "kestrel_enterprise": "enterprise" in low,
}
for k, v in traps.items():
    print(f"METRIC trap_{k}={int(v)}")
print(f"METRIC traps={sum(traps.values())}")
print(f"METRIC quotes={good}")
print(f"METRIC bad_quotes={bad}")
print(f"METRIC recommendation_ok={int(rec_ok)}")
ok = rec_ok and good >= 4 and bad == 0
print("pass" if ok else f"fail: recommendation_ok={rec_ok} quotes={good} bad_quotes={bad}")
sys.exit(0 if ok else 1)
