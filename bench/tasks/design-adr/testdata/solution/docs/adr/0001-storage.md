# 0001: Storage

## Context
The JSON file store is being replaced (see README.md).

## Options
- SQLite: embedded, transactional, no server to run.
- Postgres: a server to operate; more than we need now.
- Files with locking: keeps the current fragility.

## Decision
SQLite.

## Consequences
One file to back up; migrate to Postgres later if we need several writers across machines.
