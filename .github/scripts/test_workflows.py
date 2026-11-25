import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile
import textwrap
import unittest


ROOT = Path(__file__).resolve().parents[2]
MODULE = "github.com/sailry/sailry-harness"
REPOSITORY = "https://github.com/sailry/sailry-harness"
MOBILE_SECRETS = (
    "ANDROID_KEYSTORE_BASE64", "ANDROID_STORE_PASSWORD", "ANDROID_KEY_ALIAS", "ANDROID_KEY_PASSWORD",
    "IOS_CERTIFICATE_P12", "IOS_CERTIFICATE_PASSWORD", "IOS_PROVISIONING_PROFILE",
)
PLATFORM_SETTINGS = ("ANDROID_SIGNING_SHA256", "APPLE_API_PRIVATE_KEY", "APPLE_TEAM_ID", "APPLE_API_KEY_ID", "APPLE_API_ISSUER_ID")


class HarnessRevisionTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temporary = tempfile.TemporaryDirectory(prefix="code-workflow-fixture-")
        cls.root = Path(cls.temporary.name)
        cls.source = cls.root / "source"
        cls.source.mkdir()
        cls.git = shutil.which("git")
        cls.environment = {
            **os.environ, "GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": os.devnull,
            "GIT_ALLOW_PROTOCOL": "file", "GIT_TERMINAL_PROMPT": "0", "HOME": str(cls.root),
        }
        cls.command("init", "--quiet", "--initial-branch=main", str(cls.source))
        cls.command("-C", str(cls.source), "config", "user.name", "Workflow fixture")
        cls.command("-C", str(cls.source), "config", "user.email", "fixture@example.invalid")
        (cls.source / "source.txt").write_text("pinned\n")
        cls.command("-C", str(cls.source), "add", "source.txt")
        cls.command("-C", str(cls.source), "commit", "--quiet", "-m", "Pinned source", date="2026-11-24T01:02:03+00:00")
        cls.pinned = cls.command("-C", str(cls.source), "rev-parse", "HEAD").strip()
        cls.command("-C", str(cls.source), "tag", "v0.1.0")
        cls.command("-C", str(cls.source), "tag", "-a", "v0.1.1", "-m", "Annotated fixture")
        (cls.source / "source.txt").write_text("newer default branch\n")
        cls.command("-C", str(cls.source), "commit", "--quiet", "-am", "New default branch", date="2026-11-25T01:02:03+00:00")
        cls.head = cls.command("-C", str(cls.source), "rev-parse", "HEAD").strip()
        cls.tools = cls.root / "tools"
        cls.tools.mkdir()
        cls.tool("go", '''import json, os, sys
args = sys.argv[1:]
metadata = json.loads(os.environ["FIXTURE_METADATA"])
if args == ["mod", "edit", "-json"]:
    print(json.dumps(metadata))
elif args == ["mod", "download", "-json", "github.com/sailry/sailry-harness@" + metadata["Require"][0]["Version"]]:
    print(os.environ["FIXTURE_DEPENDENCY"])
else:
    raise SystemExit("Unexpected Go command in closed fixture: " + repr(args))
''')
        cls.tool("git", '''import json, os, subprocess, sys
from pathlib import Path
args = sys.argv[1:]
with Path(os.environ["FIXTURE_GIT_LOG"]).open("a") as log:
    log.write(json.dumps(args) + "\\n")
args = [os.environ["FIXTURE_SOURCE"] if arg == "https://github.com/sailry/sailry-harness" else arg for arg in args]
raise SystemExit(subprocess.run([os.environ["FIXTURE_GIT"], *args]).returncode)
''')

    @classmethod
    def tearDownClass(cls):
        cls.temporary.cleanup()

    @classmethod
    def command(cls, *args, date=None):
        environment = dict(cls.environment)
        if date:
            environment.update(GIT_AUTHOR_DATE=date, GIT_COMMITTER_DATE=date)
        return subprocess.check_output([cls.git, *args], env=environment, text=True)

    @classmethod
    def tool(cls, name, source):
        path = cls.tools / name
        path.write_text(f"#!{sys.executable}\n" + source)
        path.chmod(0o755)

    def resolve(self, version="v0.1.0", origin=None, replace=False, dependency_version=None):
        log = self.root / "git-log.jsonl"
        log.write_text("")
        metadata = {"Require": [{"Path": MODULE, "Version": version}]}
        if replace:
            metadata["Replace"] = [{"Old": {"Path": MODULE}, "New": {"Path": "../harness"}}]
        dependency = {"Path": MODULE, "Version": dependency_version or version}
        if origin is not None:
            dependency["Origin"] = {"Hash": origin}
        environment = {
            **self.environment, "PATH": str(self.tools) + os.pathsep + os.environ["PATH"],
            "TMPDIR": str(self.root), "FIXTURE_METADATA": json.dumps(metadata),
            "FIXTURE_DEPENDENCY": json.dumps(dependency), "FIXTURE_GIT": self.git,
            "FIXTURE_GIT_LOG": str(log), "FIXTURE_SOURCE": str(self.source),
        }
        result = subprocess.run(["bash", str(ROOT / ".github/scripts/harness-revision.sh")], env=environment, cwd=self.root, capture_output=True, text=True)
        calls = [json.loads(line) for line in log.read_text().splitlines()]
        self.assertFalse(any("HEAD" in argument for call in calls for argument in call), calls)
        return result, calls

    def assert_pinned(self, result):
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout.strip(), self.pinned)
        self.assertNotEqual(result.stdout.strip(), self.head)

    def test_origin_hash_needs_no_git_fallback(self):
        result, calls = self.resolve(origin=self.pinned)
        self.assert_pinned(result)
        self.assertEqual(calls, [])

    def test_proxy_without_origin_resolves_lightweight_tag(self):
        result, calls = self.resolve()
        self.assert_pinned(result)
        self.assertEqual(calls[0], ["ls-remote", "--exit-code", REPOSITORY, "refs/tags/v0.1.0", "refs/tags/v0.1.0^{}"])

    def test_proxy_without_origin_peels_annotated_tag(self):
        result, _ = self.resolve(version="v0.1.1")
        self.assert_pinned(result)

    def test_proxy_without_origin_resolves_pseudo_version_not_head(self):
        result, _ = self.resolve(version=f"v0.0.0-20261124010203-{self.pinned[:12]}")
        self.assert_pinned(result)

    def test_pseudo_version_timestamp_is_checked(self):
        result, _ = self.resolve(version=f"v0.0.0-20261124010204-{self.pinned[:12]}")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("timestamp", result.stderr)

    def test_unknown_revision_never_uses_head(self):
        for version in ["v9.9.9", "v0.0.0-20261124010203-000000000000", "main"]:
            with self.subTest(version=version):
                result, _ = self.resolve(version=version)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(result.stdout, "")

    def test_local_replacement_is_rejected(self):
        result, calls = self.resolve(replace=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(calls, [])

    def test_invalid_origin_or_mismatched_module_version_is_rejected(self):
        for values in [{"origin": "invalid"}, {"dependency_version": "v0.1.2"}]:
            with self.subTest(values=values):
                result, calls = self.resolve(**values)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(calls, [])


class MobilePolicyTests(unittest.TestCase):
    def policy(self, *, packages="false", upload="false", platform="both", settings=None):
        with tempfile.TemporaryDirectory(prefix="code-mobile-policy-") as temporary:
            root = Path(temporary)
            output, summary = root / "output", root / "summary"
            environment = {
                **os.environ, **{name: "" for name in (*MOBILE_SECRETS, *PLATFORM_SETTINGS)},
                **(settings or {}), "PACKAGES": packages, "UPLOAD": upload, "PLATFORM": platform,
                "GITHUB_OUTPUT": str(output), "GITHUB_STEP_SUMMARY": str(summary),
            }
            subprocess.run(["bash", str(ROOT / ".github/scripts/mobile-policy.sh")], env=environment, check=True)
            self.assertNotIn("fixture-secret-value", summary.read_text() if summary.exists() else "")
            return dict(line.split("=", 1) for line in output.read_text().splitlines())

    def test_checks_only_does_not_request_build_or_upload(self):
        self.assertEqual(set(self.policy().values()), {"false"})

    def test_missing_signing_secrets_does_not_disable_requested_builds(self):
        result = self.policy(packages="true")
        self.assertEqual(result, {"build_android": "true", "build_ios": "true", "android_upload": "false", "ios_upload": "false"})

    def test_each_of_seven_secrets_blocks_all_uploads_when_missing(self):
        settings = {name: "fixture-secret-value" for name in (*MOBILE_SECRETS, *PLATFORM_SETTINGS)}
        for missing in MOBILE_SECRETS:
            with self.subTest(missing=missing):
                result = self.policy(upload="true", settings={**settings, missing: ""})
                self.assertEqual(result["build_android"], "true")
                self.assertEqual(result["build_ios"], "true")
                self.assertEqual(result["android_upload"], "false")
                self.assertEqual(result["ios_upload"], "false")

    def test_upload_needs_explicit_request_and_existing_platform_settings(self):
        settings = {name: "fixture-secret-value" for name in (*MOBILE_SECRETS, *PLATFORM_SETTINGS)}
        result = self.policy(packages="true", settings=settings)
        self.assertEqual(result["android_upload"], "false")
        self.assertEqual(result["ios_upload"], "false")
        result = self.policy(upload="true", settings=settings)
        self.assertEqual(set(result.values()), {"true"})
        result = self.policy(upload="true", settings={**settings, "APPLE_API_PRIVATE_KEY": ""})
        self.assertEqual(result["android_upload"], "true")
        self.assertEqual(result["ios_upload"], "false")
        result = self.policy(upload="true", settings={**settings, "ANDROID_SIGNING_SHA256": ""})
        self.assertEqual(result["android_upload"], "false")
        self.assertEqual(result["ios_upload"], "true")

    def test_single_platform_never_uploads_the_other_platform(self):
        settings = {name: "fixture-secret-value" for name in (*MOBILE_SECRETS, *PLATFORM_SETTINGS)}
        result = self.policy(upload="true", platform="android", settings=settings)
        self.assertEqual(result["android_upload"], "true")
        self.assertEqual(result["build_ios"], "false")
        self.assertEqual(result["ios_upload"], "false")

    def test_build_and_upload_summary_are_independent(self):
        with tempfile.TemporaryDirectory(prefix="code-mobile-summary-") as temporary:
            output = Path(temporary) / "summary"
            environment = {
                **os.environ, "GITHUB_STEP_SUMMARY": str(output),
                "ANDROID_REQUESTED": "true", "ANDROID_RESULT": "success", "ANDROID_BUILT": "true", "ANDROID_UPLOADED": "false",
                "IOS_REQUESTED": "false", "IOS_RESULT": "skipped", "IOS_BUILT": "false", "IOS_UPLOADED": "false",
            }
            subprocess.run(["bash", str(ROOT / ".github/scripts/mobile-summary.sh")], env=environment, check=True)
            text = output.read_text()
            self.assertIn("| ANDROID | built | not uploaded |", text)
            self.assertIn("| IOS | not built (not requested) | not uploaded |", text)


class WorkflowConfigurationTests(unittest.TestCase):
    def test_go_checks_download_native_linux_runtime_before_running_tests(self):
        text = (ROOT / ".github/workflows/ci.yml").read_text()
        checks = text.split("  checks:\n", 1)[1].split("\n  native:\n", 1)[0]
        native = text.split("  native:\n", 1)[1].split("\n  macos:\n", 1)[0]
        self.assertIn("    needs: native\n", checks)
        self.assertNotIn("    needs:", native)
        self.assertIn("      SAILRY_TURSO_LIB: ${{ github.workspace }}/.runtime/native/linux-amd64/libturso_sync_sdk_kit.so\n", checks)
        self.assertIn("          name: code-native-linux-amd64\n          path: .runtime/native/linux-amd64\n", checks)
        self.assertLess(checks.index("uses: actions/download-artifact@"), checks.index("name: Verify Go test runtime"))
        self.assertLess(checks.index("name: Verify Go test runtime"), checks.index("go test ./..."))

    def test_go_runtime_verification_rejects_missing_or_corrupt_library(self):
        text = (ROOT / ".github/workflows/ci.yml").read_text()
        step = text.split("      - name: Verify Go test runtime\n", 1)[1].split("\n      - ", 1)[0]
        script = textwrap.dedent(step.split("        run: |\n", 1)[1])
        with tempfile.TemporaryDirectory(prefix="code-native-runtime-") as temporary:
            root = Path(temporary)
            library = root / "libturso_sync_sdk_kit.so"
            body = b"Closed fixture; no shared library is loaded.\n"
            library.write_bytes(body)
            manifest = root / "SHA256SUMS"
            manifest.write_text(f"{hashlib.sha256(body).hexdigest()}  {library.name}\n")
            environment = {**os.environ, "SAILRY_TURSO_LIB": str(library)}
            result = subprocess.run(["bash", "-c", script], cwd=root, env=environment, capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            library.write_bytes(body + b"corrupted")
            result = subprocess.run(["bash", "-c", script], cwd=root, env=environment, capture_output=True, text=True)
            self.assertNotEqual(result.returncode, 0)
            library.unlink()
            other = root / "unrelated.txt"
            other.write_bytes(body)
            manifest.write_text(f"{hashlib.sha256(body).hexdigest()}  {other.name}\n")
            result = subprocess.run(["bash", "-c", script], cwd=root, env=environment, capture_output=True, text=True)
            self.assertNotEqual(result.returncode, 0)

    def test_package_env_keeps_actual_telemetry_names_and_code_identity(self):
        with tempfile.TemporaryDirectory(prefix="code-package-env-") as temporary:
            root = Path(temporary)
            (root / "go.mod").write_text("module fixture\n")
            desktop = root / "cmd/sailry-code-desktop"
            desktop.mkdir(parents=True)
            (desktop / "mygo.json").write_text(json.dumps({"version": "0.1.0"}))
            harness = root / "harness"
            harness.mkdir()
            (harness / "go.mod").write_text(f"module {MODULE}\n")
            environment = {
                **os.environ, "SAILRY_HARNESS_DIR": str(harness), "SAILRY_PACKAGE_LDFLAGS": "",
                "SAILRY_SENTRY_DSN": "fixture-dsn", "SAILRY_TELEMETRY_KEY": "fixture-telemetry-key",
            }
            command = 'source "$1"; printf "%s\\n" "$SAILRY_PRODUCT" "$SAILRY_SENTRY_DSN" "$SAILRY_TELEMETRY_KEY" "$SAILRY_DESKTOP_DIR" "$SAILRY_HOST_COMMAND" "$SAILRY_RELEASE_REPOSITORY" "$SAILRY_PACKAGE_LDFLAGS"'
            result = subprocess.run(["bash", "-eu", "-c", command, "fixture", str(ROOT / "cmd/package-env.sh")], cwd=root, env=environment, capture_output=True, text=True, check=True)
            self.assertEqual(result.stdout.splitlines(), [
                "code", "fixture-dsn", "fixture-telemetry-key", "cmd/sailry-code-desktop", "./cmd/sailry-code-host",
                "https://github.com/sailry/sailry-code", f" -X {MODULE}/internal/link.ProductVersion=0.1.0",
            ])

    def test_only_actual_secret_and_variable_names_are_referenced(self):
        desktop = {"APPLE_CERTIFICATE_P12", "APPLE_CERTIFICATE_PASSWORD", "APPLE_API_PRIVATE_KEY", "SAILRY_UPDATE_SIGNING_KEY", "SAILRY_SENTRY_DSN", "SAILRY_TELEMETRY_KEY"}
        variables = {"APPLE_SIGNING_IDENTITY", "APPLE_TEAM_ID", "APPLE_API_KEY_ID", "APPLE_API_ISSUER_ID", "ANDROID_SIGNING_SHA256"}
        for path in (ROOT / ".github/workflows").glob("*.yml"):
            with self.subTest(path=path.name):
                text = path.read_text()
                self.assertLessEqual(set(re.findall(r"secrets\.([A-Za-z0-9_]+)", text)), desktop | set(MOBILE_SECRETS))
                self.assertLessEqual(set(re.findall(r"vars\.([A-Za-z0-9_]+)", text)), variables)
        release = (ROOT / ".github/workflows/release.yml").read_text()
        self.assertEqual(set(re.findall(r"secrets\.([A-Za-z0-9_]+)", release)), desktop)

    def test_mobile_is_on_demand_and_every_upload_is_gated(self):
        text = (ROOT / ".github/workflows/mobile.yml").read_text()
        self.assertIn("  workflow_dispatch:\n", text)
        self.assertNotRegex(text, r"(?m)^  (push|pull_request|workflow_call):")
        uploads = list(re.finditer(r"(?m)^      - (?:uses: actions/upload-artifact[^\n]*|name: Upload iOS to TestFlight)\n(?P<body>(?:        [^\n]*\n|          [^\n]*\n)+)", text))
        self.assertEqual(len(uploads), 3)
        for upload in uploads:
            self.assertRegex(upload["body"], r"if: needs\.policy\.outputs\.(android|ios)_upload == 'true'")


if __name__ == "__main__":
    unittest.main()
