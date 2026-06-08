<!--
Keep this section order: Tracking, Summary, Verification.
Prefer concise bullet points with concrete links, changes, and commands.
Remove any bullets that do not apply instead of leaving placeholders behind.
-->

## Tracking

- Tracks: https://konghq.atlassian.net/browse/VOL-123
- Related: https://github.com/Kong/volcano-cli/issues/45

## Summary

- add `volcano login --browser` support for interactive local auth
- persist the returned session so subsequent CLI commands reuse the same login

## Verification

- `make lint`
- `go test ./...`
- smoke-tested `volcano login --browser` against a local dev environment
