"""Check for design-search (vague prompt). Pass: docs/search-design.md exists
with >= 200 words. METRIC lines measure whether it found the causes hidden
in the search log: CJK queries never match ('simple' config does not segment
them), no stemming (plural mismatch), and typos."""
import os, re, sys

p = "docs/search-design.md"
if not os.path.exists(p):
    sys.exit("no " + p)
t = open(p, encoding="utf-8").read()
low = t.lower()
words = len(re.findall(r"\w+", t))
m = {
    "finds_cjk": bool(re.search(r"cjk|chinese|japanese|zh-tw|中文|segment|bigram|n-?gram|pg_bigm|pgroonga|tokeniz", low)),
    "finds_stemming": bool(re.search(r"stem|plural|inflect|'english'|english config|lexeme", low)),
    "finds_typos": bool(re.search(r"typo|fuzzy|trigram|pg_trgm|levenshtein|misspell|spelling", low)),
    "quantifies": bool(re.search(r"\d+(\.\d+)?\s?%", t)),
    "measures": bool(re.search(r"zero[- ]result|no[- ]result|click", low)),
}
for k, v in m.items():
    print(f"METRIC {k}={int(v)}")
print(f"METRIC insights={sum(m.values())}")
ok = words >= 200
print("pass" if ok else f"fail: words={words}")
sys.exit(0 if ok else 1)
