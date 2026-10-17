# Rubric: plan-onboarding (vague prompt; blind pairwise judge)

The prompt only says onboarding "needs to get better". The data hides one dominant problem: users on Microsoft email
domains (outlook/hotmail/live) verify at ~32% vs ~90% elsewhere, and their tickets say the verification email never
arrives or lands in junk. Compare on, in order of weight:

1. **Found the real problem** (45%): looked at the data by segment, identified the Microsoft-domain verification /
   deliverability issue, quantified it.
2. **Right priority** (25%): makes it the first item instead of generic onboarding ideas.
3. **Credible fix and measurement** (20%): deliverability work (SPF/DKIM/DMARC, sender reputation), not blocking on
   verification, a metric by domain.
4. **Clarity** (10%).

A polished plan of generic onboarding ideas that misses the data should lose to a rough plan that finds it.
