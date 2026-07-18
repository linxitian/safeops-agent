# MCP Capability Matrix

Updated: 2026-07-18. Local `policies/tools.yaml` is authoritative; all currently callable tools are L0 read-only. `CALLED` means a real structured call is present in the audited Kylin evidence; `DISCOVERED` means the target Registry initialized the server, pinged it and discovered the typed tool, but the audit does not claim a tool call.

| Domain | Tool | Real implementation | MCP protocol tested | Risk | Demo usage | Target evidence |
|---|---|---:|---:|---:|---|---:|
| system | `system.get_overview` | YES | YES | L0 | System Overview UI | CALLED |
| system | `system.get_cpu_metrics` | YES: `/proc/stat`, 200 ms | YES | L0 | CPU/memory slice | CALLED |
| system | `system.get_memory_metrics` | YES: `/proc/meminfo` | YES | L0 | CPU/memory slice | CALLED |
| system | `system.get_disk_usage` | YES: `statfs` | YES | L0 | Disk investigation | CALLED |
| system | `system.get_load_average` | YES: `/proc/loadavg` | YES | L0 | CPU investigation | CALLED |
| system | `system.get_kernel_info` | YES: uname/os-release | YES | L0 | Target probe support | CALLED |
| system | `system.get_mounts` | YES: proc mounts | YES | L0 | Disk investigation | CALLED |
| system | `system.get_uptime` | YES: proc uptime | YES | L0 | Overview | CALLED |
| process | `process.list_top` | YES: proc PID stat/status | YES | L0 | CPU investigation | CALLED |
| process | `process.search` | YES: redacted proc cmdline | YES | L0 | Process investigation | CALLED |
| process | `process.get_details` | YES: PID/start ticks identity | YES | L0 | Port/CPU investigation | CALLED |
| process | `process.get_resource_usage` | YES: CPU ticks/RSS | YES | L0 | CPU investigation | CALLED |
| process | `process.find_by_port` | YES: socket inode ↔ FD | YES | L0 | Port-conflict investigation | CALLED |
| network | `network.list_listeners` | YES: proc net tables | YES | L0 | Port-conflict investigation | CALLED |
| network | `network.list_connections` | YES: proc net tables | YES | L0 | Network investigation | CALLED |
| network | `network.check_port` | YES: socket + visible owner | YES | L0 | Port-conflict investigation | CALLED |
| network | `network.get_interfaces` | YES: Go net + proc net/dev | YES | L0 | Overview | CALLED |
| network | `network.get_interface_stats` | YES: proc net/dev | YES | L0 | Network investigation | CALLED |
| journal | `journal.get_recent` | YES: fixed bounded JSON command | YES | L0 | General investigation | CALLED |
| journal | `journal.query_unit` | YES: validated unit | YES | L0 | Port-conflict/service | CALLED |
| journal | `journal.search_errors` | YES: bounded literal filter | YES | L0 | RCA evidence | CALLED |
| journal | `journal.get_priority_events` | YES: priority 0-7 | YES | L0 | RCA evidence | CALLED |
| service | `service.get_status` | YES: fixed show properties | YES | L0 | Port-conflict/service | CALLED |
| service | `service.list_failed` | YES: fixed list-units | YES | L0 | Overview/investigation | CALLED |
| service | `service.get_dependencies` | YES: fixed properties | YES | L0 | RCA evidence | CALLED |
| service | `service.get_restart_count` | YES: NRestarts/MainPID | YES | L0 | RCA evidence | CALLED |
| diagnostic | `diagnostic.port_conflict` | YES: service/log/socket/process graph + BM25 | YES | L0 | Port-conflict investigation | CALLED |
| diagnostic | `diagnostic.high_cpu` | YES: load + CPU-tick candidates | YES | L0 | CPU investigation | CALLED |
| diagnostic | `diagnostic.disk_pressure` | YES: statfs + missing-evidence RCA | YES | L0 | Disk investigation | CALLED |
| diagnostic | `diagnostic.build_snapshot` | YES: bounded multi-source snapshot | YES | L0 | Evidence capture | CALLED |
| file | `file.list_roots` | YES: configured allowlist only | YES | L0 | File safety visibility | CALLED |
| file | `file.stat` | YES: allowlisted metadata only | YES | L0 | File investigation | CALLED |
| file | `file.list_directory` | YES: bounded directory metadata | YES | L0 | File investigation | CALLED |
| file | `file.sha256` | YES: allowlisted, 16 MiB hard bound | YES | L0 | Target/config identity | CALLED |
| file | `file.find_large` | YES: depth/result bounded, no symlink follow | YES | L0 | Disk/file investigation | CALLED |
| config | `config.list_roots` | YES: configured allowlist only | YES | L0 | Configuration visibility | CALLED |
| config | `config.get_metadata` | YES: metadata, no content | YES | L0 | Configuration investigation | CALLED |
| config | `config.snapshot` | YES: per-file/total bounded hashes | YES | L0 | Configuration change | CALLED |
| config | `config.diff_snapshot` | YES: validated baseline vs current hashes | YES | L0 | Configuration change | CALLED |

All 39 tools use typed official SDK schemas and are present in the local security catalog. Protocol evidence includes per-server in-memory initialize/list/call and official Kylin V11/loong64 Registry runs where eight installed stdio Servers initialized, responded to ping and exposed 39 fingerprints. Runtime `b5383e9` added a unique, individually bounded target call plan; checksum-verified report `target_ae6d4bbeb9ae7b8e5764` records successful structured calls for all 39 tools as the non-root `safeops` identity. Registry tests also cover disable/re-enable, rediscovery, stable fingerprints and detected tool-list changes.

The same typed Platform/SafeFS values now feed seven collector-independent Observation sources with per-collector timeout, partial-error and output budgets. This does not add tools or change the count: the MCP matrix remains exactly 8 Servers and 39 L0 tools.

Partial-evidence notes are explicit: non-root process FD ownership can be incomplete; journald depends on user ACLs and caps results at 500; commands/logs are truncated and common secret arguments redacted. Tool output never claims full visibility when permissions prevent it.

File/config reads are confined to absolute roots from the MCP Manifest; lexical and symlink escapes fail, and configuration contents are never returned. Diagnostic confidence is deterministic and preserves missing evidence instead of claiming a root cause without support.

Write names in local policy are not MCP Tools and are never exposed to the model as arbitrary execution capabilities. `safeops-privexec` has fixed dry-run handlers and an explicit `lab` mode with allowlisted file create, reversible delete-by-quarantine, quarantine/restore, service restart and SIGTERM process termination handlers. Target port/CPU/disk/file flows verified the used process termination, service restart, quarantine and restore handlers across the signed-envelope, approval, scope and target-revalidation boundary. Create/delete retain Ubuntu test evidence only, permanent purge has no handler, and no write MCP Tool is callable.

The target default uninstall/reinstall cycle preserved the Registry data and the audited 8-server/39-tool state. After restoring the root-only environment and HMAC continuity files, all eight servers were healthy and all 39 tools were rediscovered. The later full-call report independently promotes every entry to `CALLED`; it does not rely on lifecycle discovery alone.

The installed `2b26de4` target-served Web console was also traversed with real Chrome. Its Tool view rendered 8/8 healthy Servers and 39 Tools, while all 39 browser responses across the eight-view audit returned HTTP 200. This corroborates the installed Registry projection but does not replace or alter the per-tool native `CALLED` evidence above.
