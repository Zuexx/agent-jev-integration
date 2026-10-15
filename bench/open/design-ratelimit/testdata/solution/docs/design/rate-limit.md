# Per-tenant rate limiting

## Context
Three active-active regions; a tenant's limit counts across all regions. The limiter may add 2 ms p99. Inter-region
RTT is 70–180 ms, so any synchronous cross-region check is out. Regions must keep serving during partitions (up to
38 min last year). Finance needs counts within 1%.

## Options
1. Central counter in one region: exact, but 70–180 ms per request and fails during partitions. Rejected.
2. Each region enforces its own share of the limit (local quota), shares rebalanced every few seconds from gossiped
   counts. Fits 2 ms (local Redis, 0.6 ms) and survives partitions.
3. CRDT counters replicated asynchronously: similar to 2, more complex.

## Decision
Option 2. Enforcement is approximate by design; billing is separate: every gateway logs each request, logs are
aggregated asynchronously per tenant per month, which is exact and easily within 1%.

## Trade-offs
An exact global limit with 2 ms and partition tolerance is impossible. We accept overshoot: in the worst case each
region admits its full share plus one rebalance interval of drift. During a partition each region keeps its last
share (fail-open), so a tenant can get at most the sum of shares = its limit, or up to 3x if all traffic hits a region
whose share was raised just before the split; the abuse rule (≤3x for ≤1 min) holds because shares are capped at the
full limit per region and expire after 60 s without sync.

## Failure modes
- Inter-region partition: regions serve on last known shares; shares decay to limit/3 after 60 s.
- Local Redis down: fail-open with an in-process token bucket per gateway; alert.
- Clock skew: windows are per-region, so skew only affects rebalancing.
