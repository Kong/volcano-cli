# CLI download publication

`publish-cli.yml` publishes signed stable releases to the existing production
public-assets bucket through `AWS_CLI_PUBLISHER_ROLE_ARN_PRODUCTION`. Its OIDC
trust accepts stable tag jobs and its policy already allows the `builds/` prefix.
No new credentials or AWS resources are needed.

- `builds/releases/download/<tag>/`: immutable binaries, installer, signature
  bundles, and checksums; cached for one year.
- `builds/releases/latest-version`: one stable version followed by a newline;
  cached for 60 seconds and written last with an S3 conditional put.
- `builds/install.sh`: backward-compatible bootstrap; cached for 60 seconds.

All signed assets must exist and verify through the public CDN before promotion.
A partial upload keeps the previous pointer. Reruns reuse existing signatures,
reject changed signed bytes, and rebuild checksums from those canonical assets.
All stable release workflows serialize; older tags cannot downgrade the pointer.
GitHub remains a secondary release destination for older CLI versions.

The shell installer and npm downloader use `VOLCANO_DOWNLOAD_URL` only as an
explicit mirror override. The legacy `VOLCANO_GITHUB_RELEASES_URL` override keeps
its GitHub-compatible URL semantics and takes precedence. Production defaults
are tracked in code; mirrors do not fail over implicitly.

## Rollout

1. Merge the CLI change and release a new stable version. The publishing job
   verifies all five platform downloads and performs a signed Linux installation
   with GitHub download hosts blocked. Check the public bootstrap and version
   pointer after their 60-second cache lifetime.
2. Update the website's `/install` redirect, install commands, deployment
   workflows, and Hosting installation docs after the first publication succeeds.
3. Deploy the monitoring change through volcano-monitoring's deployment workflow.
   Keep the current assertions and eight probe locations. Verify two consecutive
   scheduled runs pass.

Existing binaries, published npm versions, old Homebrew formulas, and historical
GitHub URLs cannot change retrospectively; upgrading adopts the new path.
Homebrew still needs its tap, and npm still needs its registry. Signature trust
metadata can require Sigstore connectivity. This change removes GitHub release
hosting from binary downloads, not every external installation dependency.

## Recovery

Retry the failed release job with the same artifacts. Never replace an immutable
version; release a new version if signed bytes changed. A failed CDN check does
not move `latest-version`. The versioned bucket retains previous pointers and
bootstrap objects for operator recovery. To roll back a promoted release, restore
both prior objects, verify the referenced files, and wait for the 60-second cache
lifetime; CDN invalidation is optional. Older clients remain supported by GitHub
release publication. Do not repoint the installer monitor until its URL is live.
