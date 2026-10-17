"""Check for skill-release-notes: RELEASE_NOTES.md must follow the
release-notes skill exactly (the prompt does not mention the skill)."""
import os, re, sys

if not os.path.exists("RELEASE_NOTES.md"):
    sys.exit("no RELEASE_NOTES.md")
lines = open("RELEASE_NOTES.md", encoding="utf-8").read().splitlines()
errs = []
if not lines or lines[0].strip() != "# Notely 2.4.0 (2026-10-12)":
    errs.append("header")
heads = [l.strip() for l in lines if l.startswith("## ")]
if heads != ["## Breaking changes", "## New", "## Fixed", "## Internal"]:
    errs.append(f"sections {heads}")
sec, where = None, {}
for l in lines:
    if l.startswith("## "):
        sec = l[3:].strip()
    elif l.startswith("- "):
        where.setdefault(sec, []).append(l)
def has(section, *needles):
    return any(all(n.lower() in b.lower() for n in needles) for b in where.get(section, []))
expect = [("Breaking changes", "NOTE-377"), ("New", "NOTE-412"), ("New", "NOTE-401"), ("Fixed", "NOTE-398"), ("Fixed", "NOTE-405")]
for s, n in expect:
    if not has(s, n):
        errs.append(f"{n} not under {s}")
deps = [b for b in where.get("Internal", []) if b.startswith("- Updated dependencies:")]
mods = ["github.com/jackc/pgx/v5", "golang.org/x/text", "github.com/redis/go-redis/v9"]
if len(deps) != 1 or not all(m in deps[0] for m in mods) or re.search(r"\d+\.\d+\.\d+", deps[0]):
    errs.append("dependency bullet")
if not any("store" in b.lower() or "repository" in b.lower() for b in where.get("Internal", [])):
    errs.append("refactor not under Internal")
text = "\n".join(lines).lower()
if any(w in text for w in ("golangci", "flaky", "cache go modules")):
    errs.append("chore/ci/test included")
# Copied hashes are the ones in changes.txt; a copied type prefix keeps its
# syntax ("fix:", "fix(sync):", "feat!:"), unlike a sentence such as "Fixed ...".
hashes = re.findall(r"^([0-9a-f]{7}) ", open("changes.txt", encoding="utf-8").read(), re.M)
if any(h in text for h in hashes) or re.search(r"^- (feat|fix|refactor|deps|chore|ci|test)(\([^)]*\))?!?:", text, re.M):
    errs.append("hash or type prefix copied")
if any(len(l) > 100 for l in lines) or re.search("[\U0001F300-\U0001FAFF☀-➿]", text):
    errs.append("long line or emoji")
for b in sum(where.values(), []):
    if not b[2:3].isupper():
        errs.append("bullet not capitalised: " + b[:40])
        break
print(f"METRIC rule_violations={len(errs)}")
print("pass" if not errs else "fail: " + "; ".join(errs))
sys.exit(0 if not errs else 1)
