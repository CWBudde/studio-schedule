# Security policy

## Supported versions

Only the latest release gets security fixes. Fixes land on `main` (the `edge` image) and ship with
the next release; update to it as described in
[docs/operations.md](docs/operations.md#upgrades).

## Reporting a vulnerability

Please report vulnerabilities privately, never in a public issue, pull request or discussion: on
this repository's **Security** tab, choose **Report a vulnerability** (GitHub's private
vulnerability reporting). Only the maintainers see the report.

Include what you can of:

- the affected version (release, image tag or commit),
- the component (API operation, page, command) and the impact you see,
- steps or a request sequence to reproduce it, ideally against the demo data (`just serve` without
  a database, or `studio-schedule seed`),
- any fix or mitigation you suggest.

Never use real people's data in a report or a proof of concept. The answer comes in the report's
thread.

How the app meets OWASP ASVS Level 1 is in [docs/security.md](docs/security.md).
