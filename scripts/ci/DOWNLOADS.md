# CLI download publication

`publish-cli.yml` publishes signed stable releases to the existing production
public-assets bucket through `AWS_CLI_PUBLISHER_ROLE_ARN_PRODUCTION`. Its OIDC
trust accepts stable tag jobs and its policy already allows the `builds/` prefix.
No new credentials or AWS resources are needed.

- `builds/releases/download/<tag>/`: immutable binaries, installer, signature
  bundles, and checksums; cached for one year.
- `builds/releases/latest/download/<asset>`: the promoted release's assets under
  the same names, for direct links; cached for 60 seconds.
- `builds/releases/latest-version`: one stable version followed by a newline;
  cached for 60 seconds and written last with an S3 conditional put.
- `builds/install.sh`: backward-compatible bootstrap; cached for 60 seconds.

All signed assets must exist and verify through the public CDN before promotion,
and once a pointer exists the new bootstrap must install the current latest
version through it. After promotion, a signed install must resolve the public
pointer within 75 seconds of retries, which outlast its cache and CloudFront's
default error caching, or the job fails before the GitHub release is created.
A partial upload keeps the previous pointer, latest assets, and bootstrap.
If a promotion copy or the pointer write fails, promotion re-reads
`latest-version` and copies the release it names onto the latest assets and
bootstrap, best effort, then fails.
Reruns reuse existing signatures, reject changed signed bytes, and rebuild
checksums from those canonical assets.
Release runs queue and serialize across tags; older tags cannot downgrade the pointer.
The npm package, Homebrew formula, and `volcano upgrade` still download from
GitHub Releases, which publication updates only after promotion succeeds.

Promotion server-side copies each verified versioned object to the latest path,
binaries first and checksums last, and fails if a source changed after
verification. It then copies the bootstrap and conditionally writes the pointer.
The latest assets can lead the pointer for the few seconds this takes, and edge
caches can mix versions for up to 60 seconds afterward. A mixed download fails
checksum or signature verification and succeeds when retried. The installer
resolves `latest-version` once and uses versioned URLs instead.

The shell installer uses `VOLCANO_CLI_RELEASES_URL` only as an explicit mirror
override; Hosting's `VOLCANO_DOWNLOAD_URL` names the bare
download host instead. The legacy `VOLCANO_GITHUB_RELEASES_URL` override keeps
its GitHub-compatible URL semantics and takes precedence. Production defaults
are tracked in code; mirrors do not fail over implicitly.

## Rollout

1. Merge the CLI change and release a new stable version. The publishing job
   verifies all five platform downloads and performs signed Linux installations
   with GitHub download hosts blocked, the last through the new public pointer.
   Check the public bootstrap and version pointer after their 60-second cache
   lifetime.
2. Update the website's `/install` redirect, install commands, deployment
   workflows, and Hosting installation docs after the first publication succeeds.
3. Deploy the monitoring change through volcano-monitoring's deployment workflow.
   Keep the current assertions and eight probe locations. Verify two consecutive
   scheduled runs pass.
4. Switch the npm downloader, Homebrew formula, and `volcano upgrade` to
   Volcano downloads in a follow-up change.

Existing binaries and historical GitHub URLs cannot change retrospectively.
Signature trust metadata can require Sigstore connectivity. This change removes
GitHub release hosting from the shell installer's binary downloads, not every
external installation dependency.

## Recovery

Retry with "Re-run failed jobs" so the same artifacts are used; "Re-run all jobs"
rebuilds binaries with new bytes, which publication rejects for an existing
version. Never replace an immutable version; release a new version if signed
bytes changed. A failed CDN check does
not move `latest-version` or the latest assets, but a failed install through the
new pointer happens after promotion: fix the download path and rerun the job, or
roll back. If promotion fails after copying
began, rerun the job; reruns recopy the same verified objects. Until then, any
latest asset or bootstrap the log reports it could not reconcile may not match
the pointer.

To roll back, re-promote a published version with credentials that can write
`builds/` in the production bucket, a current AWS CLI v2, and `cosign`:

```sh
python3 scripts/ci/publish-downloads.py --rollback --version vMAJOR.MINOR.PATCH \
  --bucket volcano-public-assets-production
```

It re-verifies that version's signatures, CDN downloads, and installation, then
restores its latest assets, bootstrap, and pointer. Do not run it while a release
is publishing. It changes only Volcano downloads: GitHub Releases, npm,
Homebrew, and `volcano upgrade` keep their latest version, so prefer releasing a
fixed version. Wait for the 60-second cache lifetime; CDN invalidation is
optional. Older clients remain supported by GitHub release publication. Do not
repoint the installer monitor until its URL is live.
