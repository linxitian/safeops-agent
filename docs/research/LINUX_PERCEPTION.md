# Linux Perception Research

Research snapshot: 2026-07-16. References: [`node_exporter` v1.12.1](https://github.com/prometheus/node_exporter/releases/tag/v1.12.1) and [`osquery` 5.23.1](https://github.com/osquery/osquery/releases/tag/5.23.1). Neither supplies a LoongArch release, and osquery's C++/SQLite runtime conflicts with the current pure-Go deployment goal; both are design references only.

## Collector boundary

From node_exporter, adopt a registered Collector interface, injected proc/sys/root paths, explicit enable/disable, bounded concurrent collection, and collector self-observation (`duration`, `success`, `observation_count`). SafeOps extends it with Context, per-collector timeout, entity/output budgets, partial errors and completeness. High-cost systemd/process/socket and filesystem scans need filters and truncation.

Remote/FUSE `statfs` can block beyond Context cancellation. Default filters should exclude pseudo/remote mounts or isolate those reads, recording the skipped reason instead of freezing the full snapshot.

Sources: [collector registry/concurrency](https://github.com/prometheus/node_exporter/blob/v1.12.1/collector/collector.go), [path injection](https://github.com/prometheus/node_exporter/blob/v1.12.1/collector/paths.go), [filesystem filters](https://github.com/prometheus/node_exporter/blob/v1.12.1/collector/filesystem_common.go).

## Entity identities

Uniform Observation envelopes do not mean flattening all entity/event data into one number. Use kinds such as metric/entity/event/relation/signal and stable identities:

- process: boot ID + PID + start ticks (PID alone is reusable);
- socket: network namespace + socket inode; ownership can be many-to-many;
- interface: network namespace + ifindex (name can change);
- mount: mount namespace + mount ID;
- file: mount/device/inode, retaining canonical path for policy;
- journal event: boot ID + cursor.

osquery schemas illustrate process, open-file, socket, listener, interface, route, mount, systemd and file boundaries. Its Linux socket correlation walks `/proc/<pid>/fd`, groups by network namespace, parses each namespace once and joins by inode: [implementation](https://github.com/osquery/osquery/blob/5.23.1/osquery/tables/networking/linux/process_open_sockets.cpp).

Never read process environments by default. Cmdlines need length limits and secret redaction. Permission failures and process disappearance are partial observations, not whole-cycle failure or fabricated completeness.

## Platform direction

```text
LinuxPlatform typed readers/fixed command runner
-> typed snapshots
-> Collector normalizer
-> Observation batches
-> Agent / Evidence Graph / RCA / adapters
```

Fixed commands are limited to pre-discovered `systemctl`, `journalctl`, `ss` and `ip`, with independently validated arguments and no shell. Fixtures inject proc/etc roots; target reports—not assumptions—drive Kylin adapters.

