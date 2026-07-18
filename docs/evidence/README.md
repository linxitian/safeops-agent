# Target Evidence Index

This directory records the public, repository-local index for target evidence.
The authoritative narrative is
[`../target-verification-2026-07-18.md`](../target-verification-2026-07-18.md),
and the machine-readable identities and checksums are in
[`target-verification-manifest.json`](target-verification-manifest.json).

## Availability boundary

Raw target reports, Task/Trace exports, browser captures, screenshots, and
release archives are not tracked in Git. They can contain durable system
identifiers, operator configuration, or other environment-specific metadata.
They remain in the maintainers' restricted evidence archive.

Consequently, a fresh clone can verify:

- the merged source commits that produced each capability;
- the documented lineage from intermediate candidate builds through PR heads
  to squash-merged commits;
- the expected report IDs and SHA-256 values;
- the code, tests, release process, and generated-report format.

A fresh clone cannot independently recompute an external artifact SHA-256
until a maintainer supplies the corresponding artifact. Documentation must not
describe those external files as repository-contained evidence.

## Candidate commit mapping

Some native runs were made from intermediate candidate commits before their
PRs were squash-merged. Those candidates are not reachable from the default
`main` branch, but each remains an ancestor of the public GitHub pull-request
head ref. The manifest records the full candidate, final PR-head, and merged
identities:

| Candidate runtime | Final PR head | Repository commit | Scope |
|---|---|---|---|
| `b5383e9` | `a1ce22f` (PR #21) | `816f8cf` | unique native calls for all 39 MCP Tools |
| `2b26de4` | `04f1a91` (PR #23) | `7479752` | installed favicon and Chrome eight-view audit |
| `053fc2c` | `504faef` (PR #27) | `4861dcf` | native seven-Collector and adapter report |

For example, a fresh clone can retrieve the first candidate lineage with
`git fetch origin refs/pull/21/head` and then resolve the full candidate hash
recorded in the manifest. The candidate identifies the exact externally
archived runtime; the squash-merge commit identifies the resulting source on
`main`. Future target runs should prefer an exact merge commit or signed tag so
the installed and default-branch identities are the same.

## PR #57 real-agent reliability follow-up

Candidate `96e6702` from PR #57 was built for both supported Linux
architectures and installed on the official Kylin V11/LoongArch64 target.
DeepSeek V4 Flash task `task_a054a5ffc7680ae9016682c4` completed the real
`/var/log` diagnosis in 10.1 seconds with one scoped MCP call, one exact
evidence citation and a `VALID` Trace. The release archive SHA-256 was
`351ff342e1b783e092dd28a90b4efe4ba9f3e0a174c421a5fa23cf47bbcb3650`.
No Provider key is stored in this index or the restricted task evidence.
