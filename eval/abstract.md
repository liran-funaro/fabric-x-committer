# Abstract

We measure the Fabric-X transaction pipeline end to end: a Fabric-X
ordering service in front of a Fabric-X committer, on thirty-nine IBM Cloud virtual server instances
in us-east (Washington DC), all of one Intel Xeon Sapphire Rapids generation. Eighteen carry the
committer tier and its twelve-node YugabyteDB cluster on the `cx3d-64x160` profile — 64 hardware
threads, 156 GiB of RAM; twenty carry four parties of the ordering service, measured at four shards,
on `cx3d-32x80` — exactly half of one at the same clock, 32 threads and 78 GiB. The thirty-ninth runs
monitoring only. Each instance's two XFS data volumes are the profile's local instance storage,
1040 GB each on the committer machines and 520 GB on the ordering machines; fio measures 1058 MiB/s
sequential write and 102K random-write IOPS on the former and almost exactly half of both on the
latter — caps that scale with the profile rather than device characteristics. Every reported rate arrived in full,
held p99 latency under one second and held a flat in-flight count for 300 s on a fresh deployment.

The workload is deliberately conflict-free: two read–write operations per transaction over 32-byte
keys, 262 bytes serialized, signed with ECDSA P-256, and every slot takes a fresh key.

End to end, ordering included, the deployment sustains ~500K tps at a ~450 ms median and ~650 ms p99;
the same committer without an ordering service holds ~520K.
