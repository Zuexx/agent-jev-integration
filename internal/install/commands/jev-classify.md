---
description: Classify, score or triage a large dataset with Jev (Jev does the coarse pass, you review only what it surfaces)
argument-hint: "<data.csv|jsonl|json> <what to decide about each row>"
---
The user wants every row of a dataset judged with the same question(s): $ARGUMENTS

Do NOT judge the rows yourself. Use Jev through `{{JEVCC}} classify`; your job is to design the questions, calibrate them, and handle the rows Jev flags.

1. Look at the data: row count, columns, a few sample rows. Pick the column that holds the text to judge (`--field`), or send whole rows if several columns matter.
2. Write the questions as a JSON file next to the data (`<name>.questions.json`), in TypeSafe's format — a map of id → question:
   - `{"type":"choice","instructions":"...","criteria":{"option":"what it means", ...}}` for one label out of a fixed set (mutually exclusive options; add an `other` option when the list may not cover everything; up to 255 options).
   - `{"type":"score","instructions":"...","criteria":["lowest level described", "...", "highest level described"]}` for an ordered rating (2–10 descriptive levels, not bare numbers).
   - `{"type":"noul","instructions":"A statement that is true or false about the row"}` for yes/no.
   Keep each question narrow; split a multi-factor judgment into several questions and combine them later. Jev's accuracy is best in English: write the instructions in English even when the data is not.
3. Calibrate on a sample first:
   `{{JEVCC}} classify --in <file> --questions <q.json> [--field <col>] --limit 20 --out /tmp/jev-sample.jsonl`
   Read the sample output and judge those 20 rows yourself. Count how many Jev answers you agree with. If you disagree on a pattern, fix the instructions/criteria (not the data) and re-run the sample — at most 3 rounds.
4. Run everything:
   `{{JEVCC}} classify --in <file> --questions <q.json> [--field <col>] --out <name>.jev.csv`
   Add `--rank <question> --top N` to print the N highest-scoring rows when the user wants a shortlist.
5. Review only the rows with `jev_needs_review=true` (low confidence) and, if relevant, the top-ranked rows. Report the corrections you made.
6. Report: rows processed, distribution per question, Jev cost and wall time (from the summary line), your sample agreement (e.g. 18/20), how many rows you reviewed and changed, and the output path. Do not claim accuracy beyond what the sample shows.
<!-- jevcc -->
