# Rate limiting requirements (from product, security and finance)

1. Each tenant has a plan limit in requests per minute, counted across all regions combined
   (free: 600/min, team: 6,000/min, enterprise: contract value).
2. The limiter may add at most 2 ms at p99 to a request.
3. If the network between regions is down, every region must keep serving its traffic.
4. Enterprise tenants are billed for requests above their contract limit. Finance needs monthly request counts per
   tenant that are accurate to within 1%.
5. Abuse: a free tenant must not be able to exceed 3x its limit for more than a minute.
