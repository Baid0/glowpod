"""Fills in the chart's Artifact Hub annotations for a release, in place.

    python3 .github/scripts/chart-annotations.py 0.2.0 ghcr.io/owner/glowpod

- artifacthub.io/images: the image tag becomes the version being released.
- artifacthub.io/changes: that version's CHANGELOG.md section, one entry per
  bullet, its kind taken from the heading (### Added, ### Fixed, ...).
- artifacthub.io/prerelease: "true" for versions with a dash (0.2.0-rc.1).

Run from the repository root, before `helm package`.
"""

import json
import re
import sys

CHART = "deploy/helm/glowpod/Chart.yaml"
CHANGELOG = "CHANGELOG.md"
# Keep a Changelog headings → Artifact Hub change kinds.
KINDS = {
    "added": "added",
    "changed": "changed",
    "deprecated": "deprecated",
    "removed": "removed",
    "fixed": "fixed",
    "security": "security",
}


def plain(markdown: str) -> str:
    """Artifact Hub shows change descriptions as plain text."""
    text = re.sub(r"\[([^\]]+)\]\([^)]+\)", r"\1", markdown)  # [text](url) → text
    return text.replace("**", "").replace("`", "")


def changes(changelog: str, version: str) -> list[tuple[str, str]]:
    """The bullets of this version's section, as (kind, description)."""
    section = re.search(
        rf"^## \[{re.escape(version)}\][^\n]*\n(.*?)(?=^## \[|^\[[^\]]+\]: |\Z)",
        changelog,
        re.M | re.S,
    )
    if not section:
        return []
    found: list[list[str]] = []
    kind = None
    for line in section.group(1).splitlines():
        if heading := re.match(r"### (\w+)", line):
            kind = KINDS.get(heading.group(1).lower())
        elif line.startswith("- ") and kind:
            found.append([kind, line[2:].strip()])
        elif line.startswith("  ") and line.strip() and found:
            found[-1][1] += " " + line.strip()  # a bullet wrapped onto the next line
    return [(k, plain(d)) for k, d in found]


def main() -> None:
    version, image = sys.argv[1], sys.argv[2]
    chart = open(CHART, encoding="utf-8").read()

    # Everything appended below lands in annotations, so it must be the last key.
    tail = chart[chart.index("\nannotations:\n") + 1 :]
    if re.search(r"^\S", tail.split("\n", 1)[1], re.M):
        sys.exit(f"{CHART}: annotations must be the last key")

    chart, n = re.subn(
        r"^(    - name: glowpod\n      image: )\S+$", rf"\g<1>{image}:{version}", chart, flags=re.M
    )
    if n != 1:
        sys.exit(f"{CHART}: expected one glowpod entry in artifacthub.io/images")

    entries = changes(open(CHANGELOG, encoding="utf-8").read(), version)
    if entries:
        chart = chart.rstrip("\n") + "\n  artifacthub.io/changes: |\n"
        for kind, description in entries:
            # JSON strings are valid YAML: no quoting surprises from the text.
            chart += f"    - kind: {kind}\n      description: {json.dumps(description)}\n"
    if "-" in version:
        chart = chart.rstrip("\n") + '\n  artifacthub.io/prerelease: "true"\n'

    open(CHART, "w", encoding="utf-8").write(chart)
    print(f"{CHART}: {image}:{version}, {len(entries)} changes")


if __name__ == "__main__":
    main()
