---
description: Semantic lint with Jev — plain-language rules over comments, log lines and functions
argument-hint: "[paths...] [--rules rules.json]"
---
Run the Jev semantic linter, then act as the reviewer of its findings.

1. Run: `{{JEVCC}} lint $ARGUMENTS` (defaults to the current directory). Default rules: log lines that print secrets/money (error) or personal data (warn); comments that are misleading (error) or redundant (warn); function names that hide what they do (warn). A project can add rules in `.jevlint.json` (see `{{JEVCC}} lint --print-rules` for the format) — create one if the user describes a rule in words.
2. For every `error` finding, open the code and decide whether it is real. Fix the real ones (or propose the fix if the user did not ask for changes). Keep a count of false positives.
3. Summarise warnings by rule rather than listing them all, unless there are only a few.
4. Report: findings by severity, how many you confirmed vs. dismissed (this is the observed precision of the rules on this code), what you changed, and the Jev cost/time line from the output.
<!-- jevcc -->
