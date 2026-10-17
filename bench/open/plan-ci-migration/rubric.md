# Rubric: plan-ci-migration (blind pairwise judge)

The reader is the engineering manager who will staff and schedule this. Compare on, in order of weight:

1. **Hidden constraints turned into plan decisions** (40%): 6-hour hosted job limit (ml-train 540 min, data-backfill
   420 min) → self-hosted; 90-day log default vs. one-year SOC 2 rule → raise to 400 days or archive; release freeze
   2026-12-01–2027-01-05 → nothing scheduled in it; GPU repos → self-hosted GPU runners; payments → Vault via OIDC.
2. **Feasible schedule** (25%): fits two engineers at 50%, finishes before 2027-03-31, sensible order and batching.
3. **Dependencies** (20%): real ones, not a restated list of milestones.
4. **Clarity** (15%): a manager can act on it without rereading the inputs.
