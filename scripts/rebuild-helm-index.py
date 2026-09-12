#!/usr/bin/env python3
"""Rebuild a Helm repository index from published GitHub release assets."""

import argparse
import hashlib
import io
import json
import os
import re
import tarfile
from datetime import datetime, timezone
from pathlib import Path
from urllib.request import Request, urlopen


def github_json(url, token):
    headers = {"Accept": "application/vnd.github+json", "User-Agent": "opendepot-index-rebuilder"}
    if token:
        headers["Authorization"] = f"Bearer {token}"
    request = Request(url, headers=headers)
    with urlopen(request) as response:
        return json.load(response)


def download(url, token):
    headers = {"Accept": "application/octet-stream", "User-Agent": "opendepot-index-rebuilder"}
    if token:
        headers["Authorization"] = f"Bearer {token}"
    request = Request(url, headers=headers)
    with urlopen(request) as response:
        return response.read()


def chart_metadata(package):
    with tarfile.open(fileobj=io.BytesIO(package), mode="r:gz") as archive:
        chart_file = next(
            member for member in archive.getmembers() if member.name.endswith("/Chart.yaml")
        )
        metadata = archive.extractfile(chart_file).read().decode("utf-8").splitlines()
    values = {}
    for line in metadata:
        key, separator, value = line.partition(":")
        if separator and key in {"apiVersion", "appVersion", "description", "name", "type", "version"}:
            values[key] = value.strip().strip('"')
    return values


def yaml_string(value):
    return json.dumps(str(value), ensure_ascii=True)


def version_key(version):
    return tuple(int(part) if part.isdigit() else part for part in re.split(r"[.-]", version))


def build_index(releases, token, repository):
    entries = []
    for release in releases:
        if release["draft"]:
            continue
        for asset in release["assets"]:
            name = asset["name"]
            if not name.startswith("opendepot-") or not name.endswith(".tgz"):
                continue
            package = download(asset["browser_download_url"], token)
            metadata = chart_metadata(package)
            entries.append(
                {
                    "apiVersion": metadata.get("apiVersion", "v2"),
                    "appVersion": metadata.get("appVersion", ""),
                    "created": asset["updated_at"],
                    "description": metadata.get("description", ""),
                    "digest": hashlib.sha256(package).hexdigest(),
                    "name": metadata["name"],
                    "type": metadata.get("type", "application"),
                    "url": f"https://github.com/{repository}/releases/download/{release['tag_name']}/{name}",
                    "version": metadata["version"],
                }
            )

    entries.sort(key=lambda entry: version_key(entry["version"]), reverse=True)
    generated = datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")
    lines = ["apiVersion: v1", "entries:", "  opendepot:"]
    for entry in entries:
        lines.extend(
            [
                f"  - apiVersion: {yaml_string(entry['apiVersion'])}",
                f"    appVersion: {yaml_string(entry['appVersion'])}",
                f"    created: {yaml_string(entry['created'])}",
                f"    description: {yaml_string(entry['description'])}",
                f"    digest: {entry['digest']}",
                f"    name: {yaml_string(entry['name'])}",
                f"    type: {yaml_string(entry['type'])}",
                "    urls:",
                f"    - {yaml_string(entry['url'])}",
                f"    version: {yaml_string(entry['version'])}",
            ]
        )
    lines.append(f"generated: {yaml_string(generated)}")
    return "\n".join(lines) + "\n"


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--repository", default="tonedefdev/opendepot")
    parser.add_argument("--output", type=Path, default=Path("index.yaml"))
    args = parser.parse_args()
    token = os.environ.get("GITHUB_TOKEN")
    releases = github_json(
        f"https://api.github.com/repos/{args.repository}/releases?per_page=100", token
    )
    index = build_index(releases, token, args.repository)
    args.output.write_text(index, encoding="utf-8")
    print(f"Wrote {args.output} with {index.count('    version:')} chart entries")


if __name__ == "__main__":
    main()