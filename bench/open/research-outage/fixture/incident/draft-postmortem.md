# DRAFT postmortem — API outage, 2026-03-04 (written by on-call, not reviewed)

Summary: release v4.12 broke the API gateway. Customers saw errors for about an hour.

Root cause: v4.12 changed the gateway's connection pool settings. After the deploy, requests started failing.

Action items:
- Add a canary stage for gateway releases.
- Revert the connection pool change permanently.
