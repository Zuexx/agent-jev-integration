# Notely 2.4.0 (2026-10-12)

## Breaking changes
- Notes API v1 has been removed; use v2 (NOTE-377)

## New
- Search inside attachments (NOTE-412)
- Share a notebook with a link that expires (NOTE-401)

## Fixed
- Notes edited offline are no longer duplicated after reconnecting (NOTE-398)
- Pasting a table no longer moves the cursor to the end (NOTE-405)

## Internal
- Split the notes repository into read and write paths
- Updated dependencies: github.com/jackc/pgx/v5, golang.org/x/text, github.com/redis/go-redis/v9
