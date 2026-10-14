---
description: Checklist code review — Jev asks ~30 risk questions of every diff hunk, you deep-review only the flagged ones
argument-hint: "[base ref | --staged]"
---
Review the current changes in two stages.

1. Run `jevcc review $ARGUMENTS` (no argument = changes since the merge-base with the default branch, including uncommitted work). It asks every checklist question about every hunk in one Jev call per hunk and prints the findings ranked by severity × probability. Use `--checklist file.json` for a project-specific checklist (`jevcc review --print-checklist` shows the format).
2. Deep-review only the flagged hunks: read the surrounding code, and for each finding decide confirmed / dismissed with a one-line reason. Do not re-review unflagged hunks unless a confirmed finding suggests a related problem elsewhere.
3. Report confirmed issues first (file:line, what is wrong, suggested fix), then the dismissed count. Include the numbers from the output: hunks, questions asked, Jev cost and time, and the share of findings you confirmed.
<!-- jevcc -->
