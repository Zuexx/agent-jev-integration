# Onboarding plan

## What the data says
Verification is the biggest leak: of 10,000 September signups only about 7,600 verified their email. The loss is not
spread evenly. Users on Microsoft domains (outlook.com, hotmail.com, live.com) verify at about 32%, against 92% for
gmail.com and 88% for company domains. A third of new-user tickets come from these domains and say the verification
email never arrived or landed in junk. This is a deliverability problem, not a UX problem.

After verification the funnel is healthy: ~95% complete the profile and ~90% create a first project. Inviting a
teammate (~40%) is the next opportunity, but it is smaller than fixing verification.

## Plan
1. **Fix deliverability to Microsoft domains (week 1–2).** Check SPF, DKIM and DMARC alignment for the sending
   domain, register with Microsoft SNDS/JMRP, warm up a dedicated IP if we share one, and send verification mail from a
   transactional stream separate from marketing.
2. **Do not block on verification (week 2–3).** Let users start the first project and verify later; add a "resend"
   and "check your junk folder" hint after 2 minutes.
3. **Measure:** verification rate by email domain, daily. Target: Microsoft domains above 80% within a month.
4. **Then teammate invites:** prompt for invites right after the first project is created.

Expected impact: lifting Microsoft-domain verification from 32% to 85% adds roughly 1,350 verified users a month.
