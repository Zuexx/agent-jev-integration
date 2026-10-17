---
name: release-notes
description: Use when writing release notes or a changelog for Notely. Defines the team's required format; release notes that do not follow it are rejected by the release bot.
---

# Notely release notes format

Write the notes to `RELEASE_NOTES.md`.

1. First line: `# Notely <version> (<date>)`, e.g. `# Notely 2.4.0 (2026-10-12)` — version without the leading "v".
2. Sections, in exactly this order, each a `##` heading; leave out a section that would be empty:
   `## Breaking changes`, `## New`, `## Fixed`, `## Internal`.
3. Which commit goes where:
   - type with `!` (e.g. `feat!:`) → Breaking changes
   - `feat` → New
   - `fix` → Fixed
   - `refactor` → Internal
   - all `deps` commits → ONE bullet in Internal: `- Updated dependencies: <module>, <module>, ...` (module paths only, no versions)
   - `chore`, `ci`, `test` → leave out entirely
4. Each bullet: `- ` + a sentence for users starting with a capital letter, then the ticket in parentheses if the
   commit has one, e.g. `- Search inside attachments (NOTE-412)`. Do not copy the commit hash or the type prefix.
5. No emoji. No line longer than 100 characters.
