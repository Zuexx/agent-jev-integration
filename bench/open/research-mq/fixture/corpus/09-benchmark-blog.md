# We benchmarked three managed queues so you don't have to

Posted by a consultancy blog, 2025.

We ran one producer against one topic with a single partition on each service, using default settings.

| Service | Max sustained throughput |
|---|---|
| Kestrel MQ | 18,000 msg/s |
| Pipewing | 9,500 msg/s |
| Orbitline | 1,500 msg/s |

Conclusion: Orbitline cannot handle serious workloads.
