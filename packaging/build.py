#!/usr/bin/env python3
"""Build both existing app executable ABIs from one clean, pinned source commit."""
import gzip
import hashlib
import json
import os
from pathlib import Path
import subprocess
import zipfile


def run(*args, **kwargs):
    return subprocess.check_output(args, text=True, **kwargs).strip()


def file_record(path):
    return {"fileName": path.name, "sha256": hashlib.sha256(path.read_bytes()).hexdigest(), "sizeBytes": path.stat().st_size}


def main():
    root = Path(__file__).resolve().parents[1]
    os.chdir(root)
    if run("git", "status", "--porcelain", "--untracked-files=no"):
        raise SystemExit("Refusing to release a dirty tracked source tree")
    commit = run("git", "rev-parse", "HEAD")
    lock = json.loads(Path("packaging/toolchain.lock.json").read_text())
    run("git", "merge-base", "--is-ancestor", lock["upstream_commit"], commit)
    if os.environ.get("WAHOO_TOOLCHAIN_SHA256") != lock["sha256"]:
        raise SystemExit("Release requires the checksum-verified upstream Go toolchain")
    version = f'v1.19.33-wahoo.1-{commit[:12]}'
    build_time = run("git", "show", "-s", "--format=%cI", "HEAD")
    dist = root / "dist"
    dist.mkdir(exist_ok=True)
    platforms = {}
    for platform, goos, goarch, name in (
        ("linux-arm64-on-android", "linux", "arm64", "mihomo"),
        ("windows-amd64-v1", "windows", "amd64", "mihomo-windows-amd64-v1.exe"),
    ):
        output = dist / platform / name
        output.parent.mkdir(exist_ok=True)
        env = dict(os.environ, GOOS=goos, GOARCH=goarch, GOAMD64="v1", CGO_ENABLED="0", GOTOOLCHAIN="local")
        subprocess.run(["go", "build", "-tags", "with_gvisor", "-trimpath", "-ldflags",
                        f"-extldflags --static -X github.com/metacubex/mihomo/constant.Version={version} "
                        f"-X github.com/metacubex/mihomo/constant.BuildTime={build_time} -w -s -buildid=",
                        "-o", str(output), "."], env=env, check=True)
        archive = dist / f"mihomo-{platform}-{commit[:12]}.{'zip' if goos == 'windows' else 'gz'}"
        if goos == "windows":
            with zipfile.ZipFile(archive, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=9) as zipped:
                info = zipfile.ZipInfo(name, date_time=(1980, 1, 1, 0, 0, 0))
                info.compress_type = zipfile.ZIP_DEFLATED
                info.external_attr = 0o100755 << 16
                zipped.writestr(info, output.read_bytes())
        else:
            with archive.open("wb") as handle:
                with gzip.GzipFile(filename="", mode="wb", fileobj=handle, mtime=0, compresslevel=9) as zipped:
                    zipped.write(output.read_bytes())
        platforms[platform] = {"archive": file_record(archive), "executable": file_record(output)}
    manifest = {
        "schemaVersion": 1,
        "version": version,
        "source": {"repository": "https://github.com/wahoo-soft/wahoo-mihomo", "commit": commit,
                   "upstreamTag": lock["upstream_tag"], "upstreamCommit": lock["upstream_commit"]},
        "build": {"goVersion": run("go", "version"), "toolchain": lock, "tags": ["with_gvisor"],
                  "cgoEnabled": False, "buildTime": build_time},
        "platforms": platforms,
    }
    (dist / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
    (dist / "LICENSE").write_bytes((root / "LICENSE").read_bytes())
    print(json.dumps(manifest, indent=2))


if __name__ == "__main__":
    main()
