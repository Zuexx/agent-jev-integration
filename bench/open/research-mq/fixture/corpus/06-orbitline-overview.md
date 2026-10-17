# Orbitline — Overview

Orbitline is a managed log-based message queue.

## Delivery guarantees
Orbitline provides exactly-once delivery to consumers using sequence fencing: each consumer group commits a sequence number together with its offset, and the broker rejects duplicate commits.

## Regions
Orbitline is available in us-west and eu-north (Stockholm). Customer data is stored only in the selected region.
