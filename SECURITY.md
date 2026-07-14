# Security Policy

## Supported Versions

golagram is pre-v1.0; only the latest tagged release (or `main`, before the
first tag) is supported. There is no backport policy until v1.0 ships — see
[README.md's Versioning section](README.md#versioning).

## Reporting a Vulnerability

Please report security issues privately through
[GitHub Security Advisories](https://github.com/apizbe/golagram/security/advisories/new)
rather than a public issue — this gives us a chance to fix and release
before the details are public.

Include, if known:

- Affected version/commit.
- A minimal reproduction (e.g. the specific API call, update payload, or
  configuration that triggers it).
- Impact (what an attacker could do with it — token/credential exposure,
  request forgery, DoS, etc.).

We'll acknowledge reports within a few days and aim to ship a fix before
any public disclosure.

## Scope

Handled here: golagram's own code (`internal/api`, dispatch, FSM, webhook
handling, the WebApp/Login Widget signature validation in `webapp.go`, the
`storage/redis` module). Out of scope: vulnerabilities in Telegram's own
Bot API, or in a consuming application's own handler code.
