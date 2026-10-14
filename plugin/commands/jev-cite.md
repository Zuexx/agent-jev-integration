---
description: Check the citations in a research report against their sources with Jev (catches fabricated, contradicted and unsupported quotes)
argument-hint: "<report.md> [source files or folder]"
---
Check the citations in: $ARGUMENTS

1. Save every source the report cites as a local text file if it is not one already (fetched pages as .md or .txt in a `sources/` folder next to the report).
2. Write a JSONL file next to the report, one line per cited claim: `{"claim": "...", "quote": "exact text the report quotes or relies on", "source": "path/to/source.txt"}`. The quote must be copied word for word from the report's citation, not repaired.
3. Run `jevcc cite --claims <file.jsonl>`. It marks a quote that is not in its source as **fabricated** (string match), and otherwise asks Jev whether the quoted section **supports**, **contradicts** or says nothing about the claim; unsure answers are **needs review**.
4. For every result that is not verified, open the source and fix the report: correct the claim, find a real supporting quote, or remove the claim. Read the "needs review" ones yourself.
5. Report the counts per verdict and what you changed. Do not call the report verified while any claim is fabricated or contradicted.
<!-- jevcc -->
