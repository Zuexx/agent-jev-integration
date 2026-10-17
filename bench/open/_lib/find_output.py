"""Finds a task's output file wherever the agent put it: prompts that name a
bare file (findings.md, plan.md) do not say which folder."""
import os

def find_output(name):
    if os.path.exists(name):
        return name
    hits = []
    for root, dirs, files in os.walk("."):
        dirs[:] = [d for d in dirs if d not in ("node_modules", ".git")]
        if name in files:
            hits.append(os.path.join(root, name))
    return min(hits, key=len) if hits else None
