# jevcc design

Source: `docs/transcript.zh-TW.txt` (Gary Chen, "Jev + Claude"), TypeSafe docs (https://docs.typesafe.ai/llms.txt).

## What Jev is and is not

Jev (`POST https://api.typesafe.ai/v1/systemone`) answers typed questions — Choice, Score, Noul — about a `state`,
returning probabilities and confidence. It does not generate text, and the docs say plainly that it cannot be the model
behind Claude Code. Everything below therefore puts Jev at *decision points* and leaves generation to Claude.
Price: $0.042 per million input tokens, output free; rate limit 80 req/s.

## The three places from the video, in order

| # | Video | jevcc |
|---|---|---|
| 1 | Switch: Jev picks the model and the skill for every prompt | `UserPromptSubmit` hook (`jevcc hook`), toggled per session with `/jev on\|off` (handled locally, no model call). Adds a `<jev_routing>` context block. |
| 2 | Bulk classification / scoring, "Jev coarse-filters, Claude handles the essence" | `jevcc classify` + `/jev-classify` |
| 3 | Verification after coding: semantic linter, codebase scan, checklist review | `jevcc lint` + `/jev-lint`, `jevcc review` + `/jev-review` |
| – | "Have Claude read your past conversations and find Jev-able steps" | `/jev-audit` |

Not built: the head-less browser adversarial testing (needs Browser-use's Jev browser agent; out of scope).

### 1. Model routing — how it really works

A hook cannot change the session's model. Two honest mechanisms:

- **Interactive (hook arm):** the hook classifies the prompt into a tier (`haiku`/`sonnet`/`opus`, configurable) and,
  for a cheaper tier, tells Claude to delegate execution to a subagent with that `model`. The main model still reads the
  prompt and the subagent's result, so savings exist only when the delegated work is larger than the delegation
  overhead. Whether Claude follows the advice is measured (`/jev stats` → follow rate).
- **Head-less (route arm):** `jevcc ask -- <prompt>` classifies first, then runs `claude -p --model <tier>`. This is a
  true per-prompt model switch.

Skill suggestion follows the TypeSafe *Skill suggestion* cookbook: call 1 ranks every installed skill (Choice) plus gate
Nouls ("does this need a skill at all"); call 2 re-reads the top 3 with their full description and SKILL.md opening and may
reject all. The suggestion is one line; the skill index Claude Code already sends is unchanged (so this does **not** reduce
the skill-listing tokens — it targets selection errors and Claude's search time).

Low confidence → no advice (confidence-gated routing). Any error or timeout → no output; the hook never breaks claude.

## Evidence (no padding)

1. **Offline router eval** (`jevcc eval`): a labelled prompt set against a frozen skill roster. Reports tier accuracy with
   Wilson 95% CI, confusion matrix, under-routing rate (routed cheaper than the label: the quality risk) separately from
   over-routing (wasted money), skill wrong-load and needless-load rates (the cookbook's metrics), Jev latency p50/p95 and
   cost. Optional baseline: the same decision by `claude -p --model haiku`, so Jev is compared against an LLM router, not
   a straw man. Labels are hand-written; the set and its limits are stated in the report.
2. **End-to-end A/B** (`jevcc bench`): the same tasks, each in a fresh copy of a fixture, run as `off` / `hook` / `route`,
   repeated N times. Quality is a deterministic check command per task (tests, file contents), never a model's opinion.
   Cost is `total_cost_usd` from `claude -p --output-format json` **plus** Jev's own cost. Reports per-arm pass rate,
   cost, tokens, wall time, subagents, and paired cost differences with bootstrap 95% CI. If an arm is worse, the report
   says so.
3. **Live telemetry** (`/jev stats`): every routing decision is appended to `~/.jevcc/decisions-YYYY-MM-DD.jsonl` (one file per day, 14 days kept); stats reports
   tier mix, Jev latency/cost, timeouts, and how often Claude actually followed the delegation/skill advice (read from
   the session transcript).

`jevcc classify|lint|review` print their own Jev token usage, cost and wall time on every run.
