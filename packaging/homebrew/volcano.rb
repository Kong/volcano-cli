class Volcano < Formula
  desc "CLI for Volcano's hosting platform"
  homepage "https://github.com/Kong/volcano-cli"
  version "0.0.5"
  license "Apache-2.0"

  livecheck do
    url :stable
    strategy :github_latest
  end

  on_macos do
    on_arm do
      url "https://github.com/Kong/volcano-cli/releases/download/v0.0.5/volcano-macos-arm64"
      sha256 "0f1dc4cf66472801b3d9646310369f49655d0b1d11f6c129586b521a27d01f5d"
    end

    on_intel do
      url "https://github.com/Kong/volcano-cli/releases/download/v0.0.5/volcano-macos-amd64"
      sha256 "51aa01c692fe72b96cbbfbf70233ecd15bf013cd64d06737d0d82f979b5b3c2f"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/Kong/volcano-cli/releases/download/v0.0.5/volcano-linux-arm64"
      sha256 "1b48ae7cf7607587b11ae8dc0b22999c969d3b0dce8dd83b530b4b941c4034ec"
    end

    on_intel do
      url "https://github.com/Kong/volcano-cli/releases/download/v0.0.5/volcano-linux-amd64"
      sha256 "de1cb5c4435d946fd7b790365ea25f6b143378ba7d650d4798abd5a529fed634"
    end
  end

  def install
    bin.install Dir["volcano-*"].first => "volcano"
    chmod 0755, bin/"volcano"
  end

  test do
    system bin/"volcano", "--help"
  end
end
