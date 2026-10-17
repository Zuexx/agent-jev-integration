# Search: what is wrong and what to do

## What the log shows
42% of searches last week returned nothing (846 of 2,000). Breaking the zero-result searches down:

- **59% are Chinese or Japanese queries.** 84% of CJK searches return nothing. `to_tsvector('simple', ...)` splits on
  spaces and punctuation, so a CJK sentence becomes one long token and a query like 會議記錄 only matches a note whose
  whole run of text is exactly that. About 40% of our users write in zh-TW or ja, so for them search barely works.
- **16% are singular/plural mismatches** (meeting vs meetings): the 'simple' configuration does not stem.
- **16% are typos** (meetng, recipie, invocie).
- 8% are genuine no-match searches.

## Design
1. **CJK first (biggest impact).** Index CJK text as overlapping character bigrams (pg_bigm, or PGroonga), and search
   with the same tokenisation. Keep the existing tsvector for Latin text.
2. **Stemming for English.** Add a second tsvector with the 'english' configuration and match against either.
3. **Typo tolerance as a fallback.** When the full-text query returns nothing, retry with pg_trgm similarity on titles.
4. **Rank** by ts_rank plus recency instead of recency only.

## Measure
Zero-result rate by locale and click-through after search, daily. Target: CJK zero-result rate below 15%, overall
below 12%.

## Rollout
Build the new indexes concurrently, run old and new search side by side for a week and compare zero-result rates,
then switch.
