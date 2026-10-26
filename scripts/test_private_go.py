"""使用临时假令牌验证 CI 的认证、匹配范围和清理，不访问 GitHub。"""

import importlib.util
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("private_go", Path(__file__).with_name("setup-private-go.py"))
private_go = importlib.util.module_from_spec(spec)
spec.loader.exec_module(private_go)


class PrivateGoTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.runner = Path(self.temporary.name).resolve()
        self.environment = self.runner / "environment"
        self.env = {
            "RUNNER_TEMP": str(self.runner),
            "GITHUB_ENV": str(self.environment),
            "SPARQ_GO_READ_TOKEN": "test-token-only",
            "GOPRIVATE": "internal.example/*",
        }

    def configure(self):
        with patch.dict(os.environ, self.env, clear=True):
            private_go.configure()
        return dict(line.split("=", 1) for line in self.environment.read_text().splitlines())

    def test_credentials_match_only_owner_and_preserve_private_patterns(self):
        configured = self.configure()
        config = Path(configured["GIT_CONFIG_GLOBAL"])
        self.assertEqual(config.stat().st_mode & 0o777, 0o600)
        self.assertEqual(config.parent.stat().st_mode & 0o777, 0o700)
        self.assertNotIn("test-token-only", self.environment.read_text())
        self.assertEqual(configured["GOPRIVATE"], "internal.example/*,github.com/zane-xin/sparq*")
        for key in ("GONOPROXY", "GONOSUMDB"):
            self.assertEqual(configured[key], "github.com/zane-xin/sparq*")
        env = {**os.environ, "GIT_CONFIG_GLOBAL": str(config), "GIT_CONFIG_NOSYSTEM": "1"}
        own = subprocess.run(["git", "config", "--file", str(config), "--get-urlmatch", "http.extraheader", "https://github.com/zane-xin/sparq-kernel"], env=env, text=True, capture_output=True, check=True)
        self.assertTrue(own.stdout.startswith("AUTHORIZATION: basic "))
        other = subprocess.run(["git", "config", "--file", str(config), "--get-urlmatch", "http.extraheader", "https://github.com/another-owner/sparq"], env=env, text=True, capture_output=True)
        self.assertNotEqual(other.returncode, 0)
        self.assertEqual(other.stdout, "")
        with patch.dict(os.environ, {**self.env, **configured}, clear=True):
            private_go.cleanup()
            private_go.cleanup()
        self.assertFalse(config.exists())

    def test_missing_secret_stops_before_writing_credentials(self):
        with patch.dict(os.environ, {**self.env, "SPARQ_GO_READ_TOKEN": ""}, clear=True):
            with self.assertRaisesRegex(ValueError, "SPARQ_GO_READ_TOKEN"):
                private_go.configure()
        self.assertFalse(self.environment.exists())
        self.assertEqual(list(self.runner.glob("sparq-go-auth-*")), [])

    def test_environment_write_failure_removes_credentials(self):
        with patch.dict(os.environ, {**self.env, "GITHUB_ENV": str(self.runner)}, clear=True):
            with self.assertRaises(IsADirectoryError):
                private_go.configure()
        self.assertEqual(list(self.runner.glob("sparq-go-auth-*")), [])

    def test_cleanup_rejects_directory_outside_runner(self):
        sibling = self.runner / "user-files"
        sibling.mkdir()
        with patch.dict(os.environ, {**self.env, "SPARQ_GO_AUTH_DIR": str(sibling)}, clear=True):
            with self.assertRaisesRegex(ValueError, "临时目录"):
                private_go.cleanup()
        self.assertTrue(sibling.is_dir())


if __name__ == "__main__":
    unittest.main()
