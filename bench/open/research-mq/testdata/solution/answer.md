Recommendation: Orbitline

Orbitline is the only product that meets all four requirements once the newest documents are taken into account.

**Exactly-once.** Orbitline delivers exactly once to consumers:
> "Orbitline provides exactly-once delivery to consumers using sequence fencing" — [06-orbitline-overview.md]

Kestrel no longer qualifies at our budget, because exactly-once moved to the Enterprise tier:
> "Transactional consumers (exactly-once delivery) are now available only on the Enterprise tier, which has a minimum commitment of $2,000 per month." — [03-kestrel-changelog.md]

Pipewing's marketing says exactly-once, but its documentation guarantees at-least-once only:
> "Pipewing guarantees at-least-once delivery." — [05-pipewing-delivery-semantics.md]

**EU data.** 
> "Orbitline is available in us-west and eu-north (Stockholm). Customer data is stored only in the selected region." — [06-orbitline-overview.md]

**Throughput.** The blog benchmark measured a single partition, which is the per-partition limit; topics scale across partitions, so two partitions cover 2,000 msg/s:
> "We ran one producer against one topic with a single partition on each service" — [09-benchmark-blog.md]
> "A topic can have up to 64 partitions; throughput scales linearly with the number of partitions." — [10-orbitline-limits.md]

**Cost.** 2,000 msg/s is about 5,184 million messages a month. The 2024 price ($0.12/M, about $622) is superseded:
> "As of 2026-01-01, Orbitline pricing is a $120 base fee per month plus $0.05 per million messages." — [08-orbitline-pricing-update-2026.md]

That is $120 + 5,184 × $0.05 ≈ $379 per month, within the $500 limit.

**Risk.** eu-north had a 3-hour partial outage in 2025 with no data loss:
> "No acknowledged messages were lost." — [11-orbitline-incident-2025.md]
