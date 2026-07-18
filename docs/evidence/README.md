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
- the documented mapping from ephemeral candidate builds to merged commits;
- the expected report IDs and SHA-256 values;
- the code, tests, release process, and generated-report format.

A fresh clone cannot independently recompute an external artifact SHA-256
until a maintainer supplies the corresponding artifact. Documentation must not
describe those external files as repository-contained evidence.

## Candidate commit mapping

Some native runs were made from short-lived candidate commits before their PRs
were squash-merged. Those candidate objects are not present in the public Git
object graph. The manifest records both identities and the surviving merged
commit:

| Candidate runtime | Repository commit | Scope |
|---|---|---|
| `b5383e9` | `816f8cf` (PR #21) | unique native calls for all 39 MCP Tools |
| `2b26de4` | `7479752` (PR #23) | installed favicon and Chrome eight-view audit |
| `053fc2c` | `4861dcf` (PR #27) | native seven-Collector and adapter report |

The candidate hash identifies the exact externally archived runtime; the merge
commit is the source identity available to repository users. Future target
runs should prefer a merge commit or signed tag so both identities are the
same and independently resolvable.
