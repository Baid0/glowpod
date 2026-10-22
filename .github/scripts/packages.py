"""Writes the Homebrew formula and the Scoop manifest for a release.

    python3 .github/scripts/packages.py 0.2.0 glowpod-dev/glowpod checksums.txt TAP_DIR BUCKET_DIR

- TAP_DIR/Formula/glowpod.rb: `brew install glowpod-dev/tap/glowpod` (macOS
  and Linux, Intel and ARM).
- BUCKET_DIR/bucket/glowpod.json: `scoop install glowpod` (Windows, x64 and
  ARM64).

Both download the release's archives and check them against checksums.txt,
the file the release publishes with them.
"""

import json
import pathlib
import sys

# Homebrew wants under 80 characters.
DESCRIPTION = "Kubernetes dashboard for developers and platform teams, web and terminal"
HOMEPAGE = "https://glowpod.dev"


def checksums(path: str) -> dict[str, str]:
    sums = {}
    for line in pathlib.Path(path).read_text().splitlines():
        digest, _, name = line.partition("  ")
        if name:
            sums[name.strip()] = digest.strip()
    return sums


def archive(version: str, os_name: str, arch: str) -> str:
    ext = "zip" if os_name == "windows" else "tar.gz"
    return f"glowpod_{version}_{os_name}_{arch}.{ext}"


def formula(version: str, repo: str, sums: dict[str, str]) -> str:
    base = f"https://github.com/{repo}/releases/download/v{version}"

    def block(os_name: str, arch: str, indent: str) -> str:
        name = archive(version, os_name, arch)
        return f'{indent}url "{base}/{name}"\n{indent}sha256 "{sums[name]}"\n'

    return f'''# Written by .github/scripts/packages.py in {repo} on each release.
class Glowpod < Formula
  desc "{DESCRIPTION}"
  homepage "{HOMEPAGE}"
  license "MIT"

  on_macos do
    on_arm do
{block("darwin", "arm64", "      ")}    end
    on_intel do
{block("darwin", "amd64", "      ")}    end
  end

  on_linux do
    on_arm do
{block("linux", "arm64", "      ")}    end
    on_intel do
{block("linux", "amd64", "      ")}    end
  end

  def install
    bin.install "glowpod"
  end

  test do
    # 2>&1: before 0.3.0 the version went to stderr.
    assert_match version.to_s, shell_output("#{{bin}}/glowpod -version 2>&1")
  end
end
'''


def scoop(version: str, repo: str, sums: dict[str, str]) -> str:
    base = f"https://github.com/{repo}/releases/download/v{version}"
    arch = {"64bit": "amd64", "arm64": "arm64"}
    manifest = {
        "version": version,
        "description": DESCRIPTION,
        "homepage": HOMEPAGE,
        "license": "MIT",
        "architecture": {
            key: {
                "url": f"{base}/{archive(version, 'windows', a)}",
                "hash": sums[archive(version, "windows", a)],
                "extract_dir": f"glowpod_{version}_windows_{a}",
            }
            for key, a in arch.items()
        },
        "bin": "glowpod.exe",
        "checkver": {"github": f"https://github.com/{repo}"},
        "autoupdate": {
            "architecture": {
                key: {
                    "url": f"https://github.com/{repo}/releases/download/v$version/glowpod_$version_windows_{a}.zip",
                    "extract_dir": f"glowpod_$version_windows_{a}",
                }
                for key, a in arch.items()
            },
            "hash": {"url": "$baseurl/checksums.txt"},
        },
    }
    return json.dumps(manifest, indent=2) + "\n"


def main() -> None:
    version, repo, sums_path, tap, bucket = sys.argv[1:6]
    sums = checksums(sums_path)
    out = pathlib.Path(tap) / "Formula" / "glowpod.rb"
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(formula(version, repo, sums))
    out = pathlib.Path(bucket) / "bucket" / "glowpod.json"
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(scoop(version, repo, sums))
    print(f"Homebrew formula and Scoop manifest for glowpod {version}")


if __name__ == "__main__":
    main()
