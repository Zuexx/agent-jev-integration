# Rubric: research-outage (blind pairwise judge)

The reader is the team that will act on these findings. The prompt was deliberately open: the answer has to decide what
"what actually happened" requires. Compare on, in order of weight:

1. **Correct cause with evidence** (40%): expired api.internal certificate at 09:00, supported by the logs.
2. **Corrects the record** (25%): shows the draft's v4.12 theory is wrong (deploy at 09:14, rollback had no effect) and
   that the DB spike was unrelated.
3. **Why it was not prevented** (20%): the expiry warnings 7 and 3 days earlier went unanswered; useful action items.
4. **Clarity** (15%): a timeline the team can check, short and direct.
