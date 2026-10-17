# Edge API

Our public API runs active-active in three regions: us-east, eu-west and ap-southeast. Each region has its own
gateway fleet and its own Redis cluster. Tenants are routed to the nearest region by latency DNS, but a tenant's
traffic can arrive in any region.

We need per-tenant rate limiting. Read requirements.md and network.md, then write the design to
docs/design/rate-limit.md.
