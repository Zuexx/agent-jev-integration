# Rubric: design-ratelimit (blind pairwise judge)

The reader is the staff engineer who must approve this design. The requirements conflict: an exact limit across
regions cannot meet a 2 ms budget (70–180 ms RTT) and keep serving during partitions. Compare on, in order of weight:

1. **Sees and resolves the conflict** (35%): says exact global enforcement is impossible here, chooses approximate
   local enforcement with a reason, and states how much overshoot to expect.
2. **Billing separated from enforcement** (25%): exact monthly counts come from asynchronous aggregation, not from
   the limiter.
3. **Failure modes** (25%): partition behaviour, Redis loss, and the 3x/1-minute abuse bound actually shown to hold.
4. **Clarity and fit** (15%): uses the given numbers; no hand-waving.
