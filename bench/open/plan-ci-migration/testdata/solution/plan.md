# Jenkins → GitHub Actions migration plan

## Milestones
1. 2026-10-19 – 2026-10-30: org settings (log retention raised to 400 days), Vault OIDC role, self-hosted runner pool design.
2. 2026-11-02 – 2026-11-27: migrate the 35 standard services in batches of ~9 per week on GitHub-hosted runners.
3. Release freeze 2026-12-01 – 2027-01-05: no CI changes; Jenkins stays live in parallel.
4. 2027-01-18 – 2027-02-12: self-hosted GPU runners for ml-train, ml-eval, render-farm; long jobs (ml-train 540 min, data-backfill 420 min) on self-hosted runners.
5. 2027-02-15 – 2027-02-26: payments with secrets from Vault via vault-action.
6. 2027-03-01 – 2027-03-19: Jenkins read-only, archive its logs; 2027-03-31 hardware end.

## Dependencies
- Milestone 2 needs log retention and Vault from milestone 1; payments (5) needs the Vault role.
- GPU and long jobs (4) need the self-hosted pool designed in 1.

## Risks
- Jobs over 6 hours are cancelled on hosted runners → run them self-hosted (up to 5 days).
- Log retention: default 90 days < 1-year SOC 2 requirement → raise to 400 days in org settings before the first production deploy.
- Freeze leaves only ~8 working weeks before the freeze → standard services first, hard ones after.
- No GPUs on hosted runners → self-hosted GPU pool.
- payments secrets must stay in Vault → vault-action with OIDC, no CI-stored secrets.
- Capacity: two engineers at 50% → keep batches small.
