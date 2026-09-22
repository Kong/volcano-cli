# Sandbox client boundary

This handwritten adapter targets the frozen public transport **0.2.0** from
`Kong/volcano-sandboxes` revision `30a5d4ea28b951061a70d538bf0a74d3747d48ac`.
It does not import private service/guest packages or modify the Hosting-generated
client. G3/G4 will bind the SDK integration revision and validate live behavior.

Commands use the existing selected project and login token. Known production and
staging API origins map to their dedicated Sandbox origins. For maintainer tests,
`VOLCANO_SANDBOX_URL` overrides that origin; it must be HTTPS except on loopback.
This override is never inferred from a guest URL. Redirects are disabled.

Each write uses a single request key, emitted before dispatch. Network failures
are not retried. The public Operation schema carries no command result or
execution ID: pending exec therefore returns the operation and a nonzero exit
unless `--async` was explicitly requested. It does not poll an operation and
invent a successful command result. Read the execution or logs to reconcile.

Remaining C2 work needs contracts/integration, not a fake transport:

- Public PTY creation/stream/resize/cancel routes are absent (guest-only routes
  are not usable by customer credentials). `shell` explicitly fails without
  touching terminal state. Raw-mode/resize functionality and its essential
  terminal tests remain pending those public routes.
- X1 must supply the local runtime integration through existing `volcano start`
  assets; the local command currently reports `capability_disabled`.
- Custom source uploads/deployments need X2 and live G4 validation. The CLI
  exposes metadata/source preparation/deployment requests but does not invent an
  artifact upload protocol or package an application image itself.
- No live service deployment, cloud smoke, or destructive local Docker E2E was
  performed by these offline tests.
