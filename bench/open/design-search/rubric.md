# Rubric: design-search (vague prompt; blind pairwise judge)

The prompt only says "search is bad". The log and code hide the causes: 42% of searches return nothing; ~59% of those
are CJK queries that the 'simple' text-search configuration cannot match (no segmentation), ~16% are plural/singular
mismatches (no stemming) and ~16% typos. Compare on, in order of weight:

1. **Diagnosis from the data** (45%): read the log and the code, found and quantified the causes, especially CJK.
2. **Priorities** (25%): CJK first given 40% of users write zh-TW/ja.
3. **Workable design** (20%): concrete PostgreSQL options (bigram/PGroonga, 'english' config, pg_trgm), rollout and a
   metric.
4. **Clarity** (10%).

Generic search advice (Elasticsearch, "better ranking") that ignores the data should lose to a plain, data-driven design.
