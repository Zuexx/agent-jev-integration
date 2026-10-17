"""Checks a smoke run of every applicable arm (docs/eval-plan.md, pre-flight):
python3 bench/smoke.py bench-*.jsonl
Prints one line per run and a list of problems; exits 1 if there are any.
Expectations that depend on Jev's judgement (whether it advised delegating)
are reported, not asserted."""
import json, sys

RANK = {"haiku": 0, "sonnet": 1, "opus": 2}
problems, rows = [], []
for line in open(sys.argv[1]):
    r = json.loads(line)
    parts = r["arm"].split("/")
    lead, direction, open_tools = parts[0], parts[1], len(parts) == 3
    spawns, sub = r.get("spawns") or {}, r.get("subagent_tools") or {}
    up = sum(n for t, n in spawns.items() if t.startswith("jev-") and t != "jev-review" and RANK.get(t[4:], -1) > RANK[lead])
    down = sum(n for t, n in spawns.items() if t.startswith("jev-") and t != "jev-review" and RANK.get(t[4:], 9) < RANK[lead])
    review = spawns.get("jev-review", 0)
    mcp_sub = sum(n for k, n in sub.items() if "/mcp__" in k)
    skill_sub = sum(n for k, n in sub.items() if k.endswith("/Skill"))
    key = f"{r['task']} {r['arm']}"
    if r.get("error"):
        problems.append(f"{key}: error {r['error'][:120]}")
    if r.get("isolation_violations"):
        problems.append(f"{key}: isolation {r['isolation_violations']}")
    if direction == "off" and (r.get("jev_decisions") or spawns):
        problems.append(f"{key}: Jev off but decisions={r.get('jev_decisions')} spawns={spawns}")
    if direction != "off" and not r.get("jev_decisions") and not r.get("jev_errors"):
        problems.append(f"{key}: Jev on but no decision recorded")
    if direction == "down" and (up or review):
        problems.append(f"{key}: down-only arm went up={up} review={review}")
    if direction == "up" and down:
        problems.append(f"{key}: up-only arm delegated down={down}")
    if not open_tools and (mcp_sub or skill_sub):
        problems.append(f"{key}: restricted subagents used MCP={mcp_sub} Skill={skill_sub}")
    rows.append(f"{key:40} pass={str(r['pass']):5} ${r.get('claude_cost_usd', 0):.3f} jev={r.get('jev_decisions')} advice={r.get('jev_advice')} "
                f"jev_err={len(r.get('jev_errors') or [])} up={up} down={down} review={review} sub_mcp={mcp_sub} sub_skill={skill_sub} "
                f"models={sorted((r.get('model_cost_usd') or {}).keys())}")
print("\n".join(rows))
print("\nproblems:" if problems else "\nno problems", *problems, sep="\n  ")
sys.exit(1 if problems else 0)
