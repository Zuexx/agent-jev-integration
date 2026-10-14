---
description: Find steps in your past Claude Code sessions that were really classification/selection and could go to Jev
argument-hint: "[days, default 14] [all | this project]"
---
Audit the user's recent Claude Code conversations for work that Jev could have done. Arguments: $ARGUMENTS

1. Transcripts live in `~/.claude/projects/<encoded-path>/*.jsonl` (one JSON object per line; `type` is `user` or `assistant`; assistant lines carry `message.usage`). Default: this project's folder, last 14 days. Use a script (python3 or jq) to scan them; do not read whole transcripts into context.
2. Look for steps whose answer could have been enumerated in advance — the test from the video: "is this step choosing or judging, not generating?" Typical signs: labelling or filtering many items, picking one of N files/options/skills, yes/no checks repeated over a list, scoring or ranking, deciding which model/agent should handle something.
3. For each pattern found, report: what the step was (one line, with a session date), how often it occurred, the tokens the big model spent on it (sum `usage` of the assistant turns involved; say when this is an estimate), how it would be phrased for Jev (question type + options), and the estimated Jev cost ($0.042 per million input tokens).
4. Rank by savings × frequency. Be explicit about what is measured from the transcripts and what is estimated. If you find little, say so — do not inflate.
<!-- jevcc -->
