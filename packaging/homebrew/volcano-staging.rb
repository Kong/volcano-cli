# Homebrew formula for the Volcano CLI staging channel.
#
# This is the source-of-truth template for the `volcano-staging` formula in the
# Kong/homebrew-volcano tap. It installs the prebuilt, cosign-signed staging
# binary as `volcano-staging`, so it coexists on one machine with the production
# `volcano` formula.
#
# Homebrew has no channel/dist-tag concept, so staging ships as a SEPARATE
# formula rather than a `volcano@staging` selector. Because the `staging`
# release is a moving prerelease, the `url`s point at the moving `staging` tag
# and the `version` + `sha256` values below must be refreshed on every staging
# release. Until the cross-repo tap-sync automation lands, bump this file and
# copy it into the tap by hand (see VOL-515 follow-ups).
#
# A source build cannot reproduce staging: the staging device OAuth client id is
# an environment-scoped value injected at build time, and the compiled defaults
# are production, so `go build` in a public tap would bake production URLs and an
# empty client id. Homebrew must therefore ship the prebuilt binary.
class VolcanoStaging < Formula
  desc "CLI for Volcano's hosting platform (staging channel)"
  homepage "https://github.com/Kong/volcano-cli"
  # Refreshed per staging release (staging-vX.Y.Z). Placeholder until first cut.
  version "0.0.0"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/Kong/volcano-cli/releases/download/staging/volcano-macos-arm64"
      sha256 "0000000000000000000000000000000000000000000000000000000000000000"
    end

    on_intel do
      url "https://github.com/Kong/volcano-cli/releases/download/staging/volcano-macos-amd64"
      sha256 "0000000000000000000000000000000000000000000000000000000000000000"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/Kong/volcano-cli/releases/download/staging/volcano-linux-arm64"
      sha256 "0000000000000000000000000000000000000000000000000000000000000000"
    end

    on_intel do
      url "https://github.com/Kong/volcano-cli/releases/download/staging/volcano-linux-amd64"
      sha256 "0000000000000000000000000000000000000000000000000000000000000000"
    end
  end

  def install
    bin.install Dir["volcano-*"].first => "volcano-staging"
    chmod 0755, bin/"volcano-staging"
  end

  test do
    system bin/"volcano-staging", "--version"
  end
end
