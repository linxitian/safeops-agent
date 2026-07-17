# MCP Capability Matrix

Updated: 2026-07-16. Local `policies/tools.yaml` is authoritative; all currently callable tools are L0 read-only. `target_tested=NO` until a real Kylin VM report exists.

| Domain | Tool | Real implementation | MCP protocol tested | Risk | Demo usage | Target tested |
|---|---|---:|---:|---:|---|---:|
| system | `system.get_overview` | YES | YES | L0 | System Overview UI | NO |
| system | `system.get_cpu_metrics` | YES: `/proc/stat`, 200 ms | YES | L0 | CPU/memory slice | NO |
| system | `system.get_memory_metrics` | YES: `/proc/meminfo` | YES | L0 | CPU/memory slice | NO |
| system | `system.get_disk_usage` | YES: `statfs` | YES | L0 | Disk investigation | NO |
| system | `system.get_load_average` | YES: `/proc/loadavg` | YES | L0 | CPU investigation | NO |
| system | `system.get_kernel_info` | YES: uname/os-release | YES | L0 | Target probe support | NO |
| system | `system.get_mounts` | YES: proc mounts | YES | L0 | Disk investigation | NO |
| system | `system.get_uptime` | YES: proc uptime | YES | L0 | Overview | NO |
| process | `process.list_top` | YES: proc PID stat/status | YES | L0 | CPU investigation | NO |
| process | `process.search` | YES: redacted proc cmdline | YES | L0 | Process investigation | NO |
| process | `process.get_details` | YES: PID/start ticks identity | YES | L0 | Port/CPU investigation | NO |
| process | `process.get_resource_usage` | YES: CPU ticks/RSS | YES | L0 | CPU investigation | NO |
| process | `process.find_by_port` | YES: socket inode ↔ FD | YES | L0 | Port-conflict investigation | NO |
| network | `network.list_listeners` | YES: proc net tables | YES | L0 | Port-conflict investigation | NO |
| network | `network.list_connections` | YES: proc net tables | YES | L0 | Network investigation | NO |
| network | `network.check_port` | YES: socket + visible owner | YES | L0 | Port-conflict investigation | NO |
| network | `network.get_interfaces` | YES: Go net + proc net/dev | YES | L0 | Overview | NO |
| network | `network.get_interface_stats` | YES: proc net/dev | YES | L0 | Network investigation | NO |
| journal | `journal.get_recent` | YES: fixed bounded JSON command | YES | L0 | General investigation | NO |
| journal | `journal.query_unit` | YES: validated unit | YES | L0 | Port-conflict/service | NO |
| journal | `journal.search_errors` | YES: bounded literal filter | YES | L0 | RCA evidence | NO |
| journal | `journal.get_priority_events` | YES: priority 0-7 | YES | L0 | RCA evidence | NO |
| service | `service.get_status` | YES: fixed show properties | YES | L0 | Port-conflict/service | NO |
| service | `service.list_failed` | YES: fixed list-units | YES | L0 | Overview/investigation | NO |
| service | `service.get_dependencies` | YES: fixed properties | YES | L0 | RCA evidence | NO |
| service | `service.get_restart_count` | YES: NRestarts/MainPID | YES | L0 | RCA evidence | NO |
| diagnostic | `diagnostic.port_conflict` | YES: service/log/socket/process graph + BM25 | YES | L0 | Port-conflict investigation | NO |
| diagnostic | `diagnostic.high_cpu` | YES: load + CPU-tick candidates | YES | L0 | CPU investigation | NO |
| diagnostic | `diagnostic.disk_pressure` | YES: statfs + missing-evidence RCA | YES | L0 | Disk investigation | NO |
| diagnostic | `diagnostic.build_snapshot` | YES: bounded multi-source snapshot | YES | L0 | Evidence capture | NO |
| file | `file.list_roots` | YES: configured allowlist only | YES | L0 | File safety visibility | NO |
| file | `file.stat` | YES: allowlisted metadata only | YES | L0 | File investigation | NO |
| file | `file.list_directory` | YES: bounded directory metadata | YES | L0 | File investigation | NO |
| file | `file.sha256` | YES: allowlisted, 16 MiB hard bound | YES | L0 | Target/config identity | NO |
| file | `file.find_large` | YES: depth/result bounded, no symlink follow | YES | L0 | Disk/file investigation | NO |
| config | `config.list_roots` | YES: configured allowlist only | YES | L0 | Configuration visibility | NO |
| config | `config.get_metadata` | YES: metadata, no content | YES | L0 | Configuration investigation | NO |
| config | `config.snapshot` | YES: per-file/total bounded hashes | YES | L0 | Configuration change | NO |
| config | `config.diff_snapshot` | YES: validated baseline vs current hashes | YES | L0 | Configuration change | NO |

All 39 tools use typed official SDK schemas and are present in the local security catalog. Protocol evidence includes per-server in-memory initialize/list/call and a live Registry run where eight compiled stdio Servers initialized, responded to ping and exposed 39 fingerprints. Registry tests also cover disable/re-enable, rediscovery, stable fingerprints and detected tool-list changes.

The same typed Platform/SafeFS values now feed seven collector-independent Observation sources with per-collector timeout, partial-error and output budgets. This does not add tools or change the count: the MCP matrix remains exactly 8 Servers and 39 L0 tools.

Partial-evidence notes are explicit: non-root process FD ownership can be incomplete; journald depends on user ACLs and caps results at 500; commands/logs are truncated and common secret arguments redacted. Tool output never claims full visibility when permissions prevent it.

File/config reads are confined to absolute roots from the MCP Manifest; lexical and symlink escapes fail, and configuration contents are never returned. Diagnostic confidence is deterministic and preserves missing evidence instead of claiming a root cause without support.

Write names in local policy are not MCP Tools and are never exposed to the model as arbitrary execution capabilities. `safeops-privexec` has fixed dry-run handlers and an explicit `lab` mode with allowlisted file create, reversible delete-by-quarantine, quarantine/restore, service restart and SIGTERM process termination handlers. Their signed-envelope, approval, scope and target-revalidation boundaries pass Ubuntu tests; none is target-tested, permanent purge has no handler, and no write MCP Tool is callable.
