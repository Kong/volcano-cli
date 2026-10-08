---
title: "Verified domains"
description: "Prove your account owns a domain once, then attach any name below it to your frontends."
---

## What it is

A verified domain is a domain your account has proven it owns with a DNS TXT
record. Once `example.com` is verified, you can attach `example.com`,
`app.example.com`, or any other name below it to frontends in any of your
projects, with no further record.

## How it relates

- Belongs to your **account**, not a project, so the commands need an account
  token from `volcano login`. A project access token is refused.
- Every **frontend custom domain** must sit at or below a domain your account
  has verified. `volcano cloud frontends domain create` and `volcano cloud
  config deploy` print the record to publish when it is missing.
- Local development serves no custom domains, so these commands are cloud-only.

## CLI operations

| Operation | Command |
|---|---|
| Verify | `volcano cloud domains verify <domain>` |
| List | `volcano cloud domains list` |
| Remove | `volcano cloud domains remove <domain>` |

## Verify a domain

Ask for the record to publish:

```bash
volcano cloud domains verify example.com
```

```text
Error: HTTP 409: ownership of example.com is not verified

Publish this DNS record, then run the command again:
  _volcano.example.com  TXT  "volcano-domain-verification=6f0d3c1b9e2a47d58c4b1a0e9f3d2c7b"
```

Publish the record with your DNS provider and run the command again:

```bash
volcano cloud domains verify example.com
```

```text
✓ Domain 'example.com' verified
Note: Attach example.com or any name below it to your frontends. Keep the TXT record published.
```

The value is tied to your account and the domain, and it never changes.

Verify a subdomain such as `team.example.com` when only that part of the zone
is yours. A domain another account verified moves to yours once DNS serves your
record and no longer serves theirs. See
[Verify domain ownership](/platform/frontends/domain-verification) for the full
rules.

## List and remove

```bash
volcano cloud domains list
volcano cloud domains remove example.com --yes
```

Removing a domain leaves custom domains already attached below it serving.
Attaching another one needs the domain verified again.
