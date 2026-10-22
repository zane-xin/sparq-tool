#!/usr/bin/env python3
"""单仓 Go 发布检查；此文件会随各 Go 产品源码一起导出。"""

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import tarfile
import tempfile

ROOT = Path(__file__).resolve().parent.parent
SEMVER = re.compile(r"v?(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-[0-9A-Za-z.-]+)?$")


def run(*arguments, capture=False, host=False):
    env = {**os.environ, "GOWORK": "off"}
    if host:
        env.pop("GOOS", None)
        env.pop("GOARCH", None)
    env.setdefault("GOCACHE", str(ROOT / ".cache/go-build"))
    return subprocess.run(arguments, cwd=ROOT, env=env, check=True, text=True, stdout=subprocess.PIPE if capture else None)


def check_dependencies():
    config = json.loads(run("go", "mod", "edit", "-json", capture=True).stdout)
    errors = []
    for item in config.get("Require") or []:
        path, version = item["Path"], item["Version"]
        if "sparq-plugins-" in path:
            errors.append(f"公开产品不能依赖内部插件：{path}")
        if path.startswith("github.com/zane-xin/sparq") and (version == "v0.0.0" or not SEMVER.fullmatch(version) or re.search(r"\d{14}-[0-9a-f]+$", version)):
            errors.append(f"{path} 使用占位或非 release 版本 {version}；请先发布依赖仓库 tag，再以 GOWORK=off go get 更新 go.mod/go.sum")
    for item in config.get("Replace") or []:
        if not item["New"].get("Version"):
            errors.append(f"正式构建不能使用本地 replace：{item['Old']['Path']} => {item['New']['Path']}")
    if errors:
        raise ValueError("\n".join(errors))
    print(f"{config['Module']['Path']} 的正式依赖检查通过（GOWORK=off）", flush=True)
    return config["Module"]["Path"].rsplit("/", 1)[-1]


def package_tool(version):
    target_os = os.environ.get("GOOS") or run("go", "env", "GOHOSTOS", capture=True).stdout.strip()
    target_arch = os.environ.get("GOARCH") or run("go", "env", "GOHOSTARCH", capture=True).stdout.strip()
    output = ROOT / "dist"
    output.mkdir(exist_ok=True)
    archive = output / f"sparq-tool-{version}-{target_os}-{target_arch}.tar.gz"
    if archive.exists():
        raise ValueError(f"发布归档已存在，拒绝覆盖：{archive}")
    with tempfile.TemporaryDirectory(prefix=".release-", dir=output) as temporary:
        staging = Path(temporary)
        for name in ["sparq-tool", "sparq-updater"]:
            binary = staging / (name + (".exe" if target_os == "windows" else ""))
            run("go", "build", "-trimpath", "-o", str(binary), f"./cmd/{name}")
        metadata = {"version": version, "os": target_os, "arch": target_arch, "modules": run("go", "list", "-m", "-f", "{{.Path}}@{{.Version}}", "all", capture=True).stdout.splitlines()}
        (staging / "release.json").write_text(json.dumps(metadata, indent=2) + "\n")
        packed = staging / archive.name
        with tarfile.open(packed, "w:gz") as writer:
            for path in sorted(staging.iterdir()):
                if path != packed:
                    writer.add(path, arcname=path.name, recursive=False)
        digest = hashlib.sha256(packed.read_bytes()).hexdigest()
        packed.rename(archive)
        archive.with_name(archive.name + ".sha256").write_text(f"{digest}  {archive.name}\n")
    print(archive)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=["check", "release"])
    parser.add_argument("--version", default="")
    args = parser.parse_args()
    name = check_dependencies()
    if args.action == "release" and not SEMVER.fullmatch(args.version):
        raise ValueError("release 必须传入正式版本，例如 VERSION=0.1.0")
    run("go", "mod", "verify")
    run("go", "test", "./...", host=True)
    run("go", "vet", "./...")
    run("go", "build", "./...")
    if args.action == "release":
        version = args.version.removeprefix("v")
        if name == "sparq-tool":
            package_tool(version)
        else:
            print(f"{name} {version} 已通过单仓发布检查；提交 go.mod/go.sum 后在本仓库创建 v{version} tag 发布源码")


if __name__ == "__main__":
    try:
        main()
    except (ValueError, OSError, subprocess.CalledProcessError) as error:
        raise SystemExit(str(error))
