# Security Policy

## Supported Versions

`unch` is a fast-moving CLI, so security fixes are only guaranteed for the
latest stable release line and the current `main` branch.

| Version | Supported |
| --- | --- |
| `v0.3.x` | Yes |
| older releases | No |
| `main` | Best effort |

If you are reporting a vulnerability against an older release, please confirm
whether it still reproduces on the latest stable version before filing.

## Reporting a Vulnerability

Please do **not** open a public GitHub issue for suspected security problems.

Preferred disclosure path:

1. Use GitHub's private vulnerability reporting / security advisory flow for
   this repository if it is available.
2. If private reporting is not available, contact the maintainer privately
   through GitHub and include enough detail to reproduce the issue.

When reporting, include:

- affected `unch` version or commit
- OS and architecture
- whether the issue affects local indexing, remote sync, release artifacts, or
  GitHub Actions workflows
- clear reproduction steps or a minimal repository
- expected impact

You can expect:

- acknowledgement after the report is triaged
- a request for clarification if reproduction is incomplete
- a coordinated fix and disclosure once the issue is understood

## Scope

This policy covers:

- the `unch` CLI and local indexing/search code
- release artifacts published from this repository
- GitHub Actions workflows shipped with `unch`
- generated CI scaffolding produced by `unch create ci`

Third-party services, GitHub-hosted infrastructure, and vulnerabilities in
upstream dependencies may need to be reported upstream as well.

