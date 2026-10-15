# Pipewing documentation: delivery semantics

Pipewing guarantees at-least-once delivery. A message can be delivered more than once after a consumer crash or a network timeout.

Exactly-once processing is the application's responsibility: attach an idempotency key to each message and use the Pipewing SDK's deduplication helper, which remembers keys for 24 hours.
