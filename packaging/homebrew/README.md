# Homebrew packaging

`volcano.rb` is the source formula for the `kong-volcano/homebrew-tap` repository.

To publish or update Homebrew support:

1. Publish a stable GitHub Release from this repo.
2. Download the release `SHA256SUMS` asset.
3. Update `volcano.rb` with the new version, asset URLs, and SHA256 values.
4. Copy `volcano.rb` to `Formula/volcano.rb` in `git@github.com:kong-volcano/homebrew-tap.git`.
5. Open a PR in the tap repo.

Users install the formula with:

```bash
brew install kong-volcano/tap/volcano
```
