#!/usr/bin/env python3
"""为 GitHub Actions 配置临时的跨仓库 Go 依赖读取凭据。"""

import base64
import os
from pathlib import Path
import shutil
import sys
import tempfile


def configure():
    token = os.environ.get("SPARQ_GO_READ_TOKEN", "")
    if not token:
        raise ValueError("缺少 Actions Secret SPARQ_GO_READ_TOKEN；请配置可读取 Sparq 私有依赖仓库 Contents 的令牌，当前仓库的 GITHUB_TOKEN 无法跨私有仓库下载模块。")
    runner = Path(os.environ["RUNNER_TEMP"]).resolve()
    environment = Path(os.environ["GITHUB_ENV"])
    directory = Path(tempfile.mkdtemp(prefix="sparq-go-auth-", dir=runner))
    try:
        header = base64.b64encode(f"x-access-token:{token}".encode()).decode()
        config = directory / "gitconfig"
        with open(config, "x", opener=lambda path, flags: os.open(path, flags, 0o600)) as output:
            output.write(f'[http "https://github.com/zane-xin/"]\n\textraheader = AUTHORIZATION: basic {header}\n')
        with environment.open("a") as output:
            output.write(f"GIT_CONFIG_GLOBAL={config}\nSPARQ_GO_AUTH_DIR={directory}\n")
            for key in ("GOPRIVATE", "GONOPROXY", "GONOSUMDB"):
                previous = os.environ.get(key, "")
                patterns = [part for part in previous.split(",") if part]
                if "github.com/zane-xin/sparq*" not in patterns:
                    patterns.append("github.com/zane-xin/sparq*")
                output.write(f"{key}={','.join(patterns)}\n")
        print("已配置临时 Go 私有依赖读取凭据。")
    except BaseException:
        shutil.rmtree(directory)
        raise


def cleanup():
    value = os.environ.get("SPARQ_GO_AUTH_DIR")
    if not value:
        return
    directory = Path(value)
    runner = Path(os.environ["RUNNER_TEMP"]).resolve()
    if directory.parent != runner or not directory.name.startswith("sparq-go-auth-") or directory.is_symlink():
        raise ValueError("凭据清理目录不属于本次 Runner 临时目录")
    if directory.exists():
        shutil.rmtree(directory)


if __name__ == "__main__":
    try:
        if sys.argv[1:] == ["cleanup"]:
            cleanup()
        elif sys.argv[1:]:
            raise ValueError("用法：python3 scripts/setup-private-go.py [cleanup]")
        else:
            configure()
    except (ValueError, KeyError, OSError) as error:
        raise SystemExit(str(error))
