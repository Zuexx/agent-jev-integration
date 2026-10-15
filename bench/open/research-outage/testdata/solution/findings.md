# What happened on 2026-03-04

**Cause: the TLS certificate for api.internal expired at 09:00:00 UTC.** From 09:00:02 the gateway rejected every
upstream connection with `x509: certificate has expired`. Errors stopped at 10:06, one minute after jun renewed the
certificate at 10:04:51.

**The draft postmortem is wrong about v4.12.** Errors started at 09:00, but v4.12 was deployed at 09:14, and rolling
back at 09:40 had no effect (still 100% errors at 09:41). The deploy was a coincidence.

**The DB CPU spike is unrelated.** orders-primary hit 93% at 08:50 and was back to normal at 08:55, before the outage began.

**Why it happened:** the cert monitor warned #platform-alerts 7 days and 3 days before expiry, and the warnings went unanswered.

Timeline (UTC): 09:00 cert expires, errors begin · 09:14 v4.12 deployed · 09:40 rollback, no change · 10:04 cert renewed · 10:06 errors gone.

Action items: automate renewal for api.internal; page (not just post) on expiry warnings; correct the draft postmortem.
