#!/usr/bin/env python3
"""Generate app locks only after Harbor resolves the published OCI digest."""
import json
from pathlib import Path
import re
import sys


def main():
    repository, digest = sys.argv[1:]
    if not re.fullmatch(r"sha256:[0-9a-f]{64}", digest):
        raise SystemExit("Invalid OCI digest")
    manifest = json.loads(Path("dist/manifest.json").read_text())
    source = manifest["source"]
    common = dict(schemaVersion=1, repository=repository, digest=digest,
                  tag="v1.19.33-wahoo.1-" + source["commit"][:12], source=source,
                  version=manifest["version"], variant="wahoo_sticky_urltest")
    android = manifest["platforms"]["linux-arm64-on-android"]
    windows = manifest["platforms"]["windows-amd64-v1"]
    locks = Path("dist/locks")
    locks.mkdir(exist_ok=True)
    (locks / "mihomo-core.lock.json").write_text(json.dumps(dict(common,
        name="mihomo", platform="linux-arm64-on-android", abi="arm64-v8a",
        upstream_version=source["upstreamTag"], upstream_commit=source["upstreamCommit"],
        archive=android["archive"], sha256=android["executable"]["sha256"], size=android["executable"]["sizeBytes"],
        output="app/src/main/jniLibs/arm64-v8a/libmihomo.so"), indent=2) + "\n")
    executable = dict(windows["executable"], archivePath=windows["executable"]["fileName"],
                      packagePath="src/Winui3V2ray/bin/Mihomo/mihomo-windows-amd64-v1.exe")
    (locks / "mihomo.lock.json").write_text(json.dumps(dict(common,
        runtime="mihomo", platform="windows-amd64-v1", archive=windows["archive"], executable=executable), indent=2) + "\n")


if __name__ == "__main__":
    main()
