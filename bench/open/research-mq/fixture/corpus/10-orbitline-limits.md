# Orbitline service limits

- Each partition sustains up to 1,500 messages per second.
- A topic can have up to 64 partitions; throughput scales linearly with the number of partitions.
- Exactly-once delivery applies per partition; ordering is guaranteed within a partition.
