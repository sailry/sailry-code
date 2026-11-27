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
        cls.jq = shutil.which("jq")
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
        cls.tool("jq", '''import os, subprocess, sys
result = subprocess.run([os.environ["FIXTURE_JQ"], *sys.argv[1:]], input=sys.stdin.buffer.read(), capture_output=True)
output = result.stdout
if os.environ.get("FIXTURE_JQ_CRLF") == "1":
    output = output.replace(b"\\n", b"\\r\\n")
sys.stdout.buffer.write(output)
sys.stderr.buffer.write(result.stderr)
raise SystemExit(result.returncode)
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

    def resolve(self, version="v0.1.0", origin=None, replace=False, dependency_version=None, crlf=False):
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
            "FIXTURE_JQ": self.jq, "FIXTURE_JQ_CRLF": "1" if crlf else "0",
        }
        result = subprocess.run(["bash", str(ROOT / ".github/scripts/harness-revision.sh")], env=environment, cwd=self.root, capture_output=True, text=True)
        calls = [json.loads(line) for line in log.read_text().splitlines()]
        self.assertFalse(any("HEAD" in argument for call in calls for argument in call), calls)
        return result, calls

    def assert_pinned(self, result):
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, self.pinned + "\n")
        self.assertNotEqual(result.stdout.strip(), self.head)

    def test_origin_hash_needs_no_git_fallback(self):
        result, calls = self.resolve(origin=self.pinned)
        self.assert_pinned(result)
        self.assertEqual(calls, [])

    def test_native_windows_jq_crlf_preserves_exact_version_and_revision(self):
        for values in [{"origin": self.pinned}, {}, {"version": "v0.1.1"},
                       {"version": f"v0.0.0-20261124010203-{self.pinned[:12]}"}]:
            with self.subTest(values=values):
                result, _ = self.resolve(crlf=True, **values)
                self.assert_pinned(result)

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


class MobileReleaseTests(unittest.TestCase):
    def step_script(self, workflow, name):
        text = (ROOT / f".github/workflows/{workflow}.yml").read_text()
        step = re.split(r"\n(?=      - |  [^ ])", text.split(f"      - name: {name}\n", 1)[1], maxsplit=1)[0]
        return textwrap.dedent(step.split("        run: |\n", 1)[1])

    def test_release_tag_desktop_and_mobile_build_agree(self):
        script = self.step_script("release", "Code tag, product and mobile metadata agree")
        with tempfile.TemporaryDirectory(prefix="code-release-version-") as temporary:
            root = Path(temporary)
            identity = root / "internal/product/identity.go"
            desktop = root / "cmd/sailry-code-desktop/mygo.json"
            mobile = root / "apps/mobile/pubspec.yaml"
            for path in [identity, desktop, mobile]:
                path.parent.mkdir(parents=True)
            identity.write_text('const (\n\tID = "code"\n\tVersion = "9.8.7"\n)\n')
            desktop.write_text('{"version": "9.8.7"}\n')
            output = root / "output"
            environment = {**os.environ, "GITHUB_REF_TYPE": "tag", "GITHUB_REF_NAME": "v9.8.7", "GITHUB_OUTPUT": str(output)}

            mobile.write_text("version: 9.8.7+42\n")
            result = subprocess.run(["bash", "-c", script], env=environment, cwd=root, capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(output.read_text(), "version=9.8.7\nmobile_build=42\n")
            output.unlink()

            for version in ["9.8.6+42", "9.8.7", "9.8.7+0", "9.8.7+42+43", "9.8.7+bad"]:
                with self.subTest(version=version):
                    mobile.write_text(f"version: {version}\n")
                    result = subprocess.run(["bash", "-c", script], env=environment, cwd=root, capture_output=True, text=True)
                    self.assertNotEqual(result.returncode, 0)
                    self.assertFalse(output.exists())

    def test_release_fails_when_either_distribution_upload_is_disabled(self):
        script = self.step_script("mobile", "Require both mobile distribution uploads for a Code release")
        for android, ios in [("true", "true"), ("true", "false"), ("false", "true"), ("false", "false")]:
            with self.subTest(android=android, ios=ios):
                environment = {**os.environ, "ANDROID_UPLOAD": android, "IOS_UPLOAD": ios}
                result = subprocess.run(["bash", "-c", script], env=environment, capture_output=True, text=True)
                self.assertEqual(result.returncode == 0, android == ios == "true", result.stderr)

    def test_mobile_draft_requires_two_packages_and_checksums_their_release_names(self):
        script = self.step_script("release", "Mobile packages and checksums")
        packages = {"Sailry-android-arm64.apk": b"fixture APK", "Sailry-ios.ipa": b"fixture IPA"}
        with tempfile.TemporaryDirectory(prefix="code-release-mobile-assets-") as temporary:
            root = Path(temporary)
            mobile, assets = root / "mobile", root / "assets"
            mobile.mkdir()
            assets.mkdir()
            for name, content in packages.items():
                with self.subTest(missing=name):
                    (mobile / name).write_bytes(b"")
                    result = subprocess.run(["bash", "-c", script], cwd=assets, capture_output=True, text=True)
                    self.assertNotEqual(result.returncode, 0)
                    self.assertEqual(list(assets.iterdir()), [])
                    (mobile / name).write_bytes(content)
            result = subprocess.run(["bash", "-c", script], cwd=assets, capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            checksums = (assets / "SHA256SUMS-mobile").read_text().splitlines()
            self.assertEqual(checksums, [f"{hashlib.sha256(content).hexdigest()}  {name}" for name, content in packages.items()])
            for name, content in packages.items():
                self.assertEqual((assets / name).read_bytes(), content)


class WorkflowConfigurationTests(unittest.TestCase):
    def test_only_distribution_packages_receive_telemetry_and_are_uploaded(self):
        workflow = (ROOT / ".github/workflows/ci.yml").read_text()
        for name in ["macos", "windows", "host-linux"]:
            job = re.split(r"\n(?=  [^ ])", workflow.split(f"\n  {name}:\n", 1)[1], maxsplit=1)[0]
            with self.subTest(job=name):
                for secret in ["SAILRY_SENTRY_DSN", "SAILRY_TELEMETRY_KEY"]:
                    self.assertIn(f"      {secret}: ${{{{ inputs.distribution && secrets.{secret} || '' }}}}", job)
                upload = job.split("      - uses: actions/upload-artifact@", 1)[1]
                self.assertIn("        if: inputs.distribution\n", upload)
        action = (ROOT / ".github/actions/native-runtime/action.yml").read_text()
        upload = action.split("    - uses: actions/upload-artifact@", 1)[1]
        self.assertNotIn("if: inputs.distribution", upload)

    def test_mobile_policy_does_not_receive_credentials_without_upload_request(self):
        workflow = (ROOT / ".github/workflows/mobile.yml").read_text()
        policy = workflow.split("\n  policy:\n", 1)[1].split("\n  checks:\n", 1)[0]
        for secret in [*MOBILE_SECRETS, "APPLE_API_PRIVATE_KEY"]:
            with self.subTest(secret=secret):
                self.assertIn(f"          {secret}: ${{{{ (inputs.upload || inputs.release_version != '') && secrets.{secret} || '' }}}}", policy)

    def test_integration_suites_have_database_fixtures_and_release_coverage(self):
        workflow = (ROOT / ".github/workflows/ci.yml").read_text()
        job = workflow.split("\n  integration:\n", 1)[1].split("\n  repository-integration:\n", 1)[0]
        self.assertIn("    needs: native-linux\n", job)
        self.assertIn("    if: inputs.integration || inputs.distribution\n", job)
        self.assertIn("    runs-on: ubuntu-24.04\n", job)
        self.assertIn("      - uses: ./.github/actions/harness\n", job)
        self.assertIn("          name: code-native-linux-amd64\n", job)
        self.assertIn("sha256sum --check SHA256SUMS", job)
        self.assertIn('test -f "$SAILRY_TURSO_LIB"', job)
        for engine, port in [("postgres", 5432), ("mysql", 3306)]:
            prefix = f"SAILRY_TEST_{engine.upper()}_"
            self.assertIn(f"      {prefix}HOST: 127.0.0.1\n", job)
            self.assertIn(f"          {prefix}PORT: ${{{{ job.services.{engine}.ports['{port}'] }}}}\n", job)
            self.assertIn(f"      {prefix}TLS: disable\n", job)
            for setting in ["DATABASE", "USERNAME", "PASSWORD"]:
                self.assertRegex(job, rf"(?m)^      {prefix}{setting}: .+$")
            self.assertIn(f"      {engine}:\n        image:", job)
        step = job.split("      - name: SSH and database integration tests\n", 1)[1]
        script = textwrap.dedent(step.split("        run: |\n", 1)[1])
        packages = ["./modules/code/ssh/service", "./modules/code/databases/service"]
        for package in packages:
            self.assertTrue(any((ROOT / package).glob("*integration_test.go")), package)
        with tempfile.TemporaryDirectory(prefix="code-integration-entry-") as temporary:
            root = Path(temporary)
            tool = root / "go"
            tool.write_text(f"#!{sys.executable}\n" + '''import json, os, sys
from pathlib import Path
Path(os.environ["FIXTURE_OUTPUT"]).write_text(json.dumps(sys.argv[1:]))
raise SystemExit(int(os.environ["FIXTURE_STATUS"]))
''')
            tool.chmod(0o755)
            output = root / "arguments.json"
            environment = {**os.environ, "PATH": str(root) + os.pathsep + os.environ["PATH"], "FIXTURE_OUTPUT": str(output)}
            for status in [0, 37]:
                result = subprocess.run(["bash", "-c", script], cwd=ROOT,
                                        env={**environment, "FIXTURE_STATUS": str(status)}, capture_output=True, text=True)
                self.assertEqual(result.returncode, status, result.stderr)
                self.assertEqual(json.loads(output.read_text()), ["test", "-p", "1", "-timeout", "20m", "-tags", "integration", *packages])
        release = (ROOT / ".github/workflows/release.yml").read_text()
        self.assertIn("      distribution: true\n", release)

    def test_repository_integration_preserves_engine_platform_coverage(self):
        workflow = (ROOT / ".github/workflows/ci.yml").read_text()
        job = workflow.split("\n  repository-integration:\n", 1)[1].split("\n  native:\n", 1)[0]
        self.assertIn("    needs: [native-linux, native]\n", job)
        self.assertIn("    if: inputs.integration || inputs.distribution\n", job)
        for target in ["linux-amd64", "darwin-arm64", "windows-amd64"]:
            self.assertIn(f"target: {target}, turso:", job)
        self.assertIn("setup-linux-command-sandbox.sh", job)
        self.assertLess(job.index("go mod download"), job.index("export GOPROXY=off"))
        self.assertIn("          name: code-native-${{ matrix.target }}\n", job)
        self.assertIn('test -f "$SAILRY_TURSO_LIB"', job)
        script = textwrap.dedent(job.split("      - name: Git engine and Node module integration tests\n", 1)[1].split("        run: |\n", 1)[1])
        engine = "./modules/code/git/service"
        modules = ["./modules/code/git/node", "./modules/code/worktrees/node"]
        for package in [engine, *modules]:
            self.assertTrue(any((ROOT / package).glob("*integration_test.go")), package)
        with tempfile.TemporaryDirectory(prefix="code-repository-integration-") as temporary:
            tool = Path(temporary) / "go"
            tool.write_text(f"#!{sys.executable}\n" + '''import json, os, sys
print(json.dumps([os.environ.get("GOPROXY"), sys.argv[1:]]))
''')
            tool.chmod(0o755)
            env = {**os.environ, "PATH": temporary + os.pathsep + os.environ["PATH"]}
            for platform in ["Linux", "macOS", "Windows"]:
                result = subprocess.run(["bash", "-c", script], cwd=ROOT,
                                        env={**env, "RUNNER_OS": platform}, capture_output=True, text=True)
                self.assertEqual(result.returncode, 0, result.stderr)
                packages = [engine, *modules] if platform == "Linux" else [engine]
                self.assertEqual(json.loads(result.stdout), ["off", ["test", "-p", "1", "-timeout", "20m", "-tags", "integration", *packages]])

    def test_go_checks_download_native_linux_runtime_before_running_tests(self):
        text = (ROOT / ".github/workflows/ci.yml").read_text()
        native_linux = text.split("  native-linux:\n", 1)[1].split("\n  checks:\n", 1)[0]
        checks = text.split("  checks:\n", 1)[1].split("\n  integration:\n", 1)[0]
        native = text.split("  native:\n", 1)[1].split("\n  macos:\n", 1)[0]
        self.assertIn("    needs: [scope, native-linux]\n", checks)
        self.assertIn("          target: linux-amd64\n", native_linux)
        self.assertNotIn("linux-amd64", native)
        self.assertIn("      SAILRY_TURSO_LIB: ${{ github.workspace }}/.runtime/native/linux-amd64/libturso_sync_sdk_kit.so\n", checks)
        self.assertIn("          name: code-native-linux-amd64\n          path: .runtime/native/linux-amd64\n", checks)
        self.assertLess(checks.index("uses: actions/download-artifact@"), checks.index("name: Verify Go test runtime"))
        self.assertLess(checks.index("name: Verify Go test runtime"), checks.index("go test ./..."))
        self.assertIn("go vet -tags integration ./...", checks)
        self.assertIn("go test -race", checks)

    def test_platform_tests_cover_macos_and_windows_os_packages(self):
        text = (ROOT / ".github/workflows/ci.yml").read_text()
        job = text.split("\n  platform-tests:\n", 1)[1].split("\n  macos:\n", 1)[0]
        self.assertIn("    needs: [scope, native]\n", job)
        for target in ["darwin-arm64", "windows-amd64"]:
            self.assertIn(f"target: {target}", job)
        for pkg in ["./modules/code/git/service", "./modules/code/databases/service", "./modules/code/terminal/node"]:
            self.assertIn(pkg, job)

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
        self.assertEqual(set(re.findall(r"secrets\.([A-Za-z0-9_]+)", release)), desktop | set(MOBILE_SECRETS))

    def test_mobile_is_manual_or_called_by_release_and_every_upload_is_gated(self):
        text = (ROOT / ".github/workflows/mobile.yml").read_text()
        self.assertIn("  workflow_dispatch:\n", text)
        self.assertIn("  workflow_call:\n", text)
        self.assertNotRegex(text, r"(?m)^  (push|pull_request):")
        uploads = list(re.finditer(r"(?m)^      - (?:uses: actions/upload-artifact[^\n]*|name: Upload iOS to TestFlight)\n(?P<body>(?:        [^\n]*\n|          [^\n]*\n)+)", text))
        self.assertEqual(len(uploads), 3)
        for upload in uploads:
            self.assertRegex(upload["body"], r"if: needs\.policy\.outputs\.(android|ios)_upload == 'true'")
        self.assertIn("name: code-mobile-android\n          path: ${{ runner.temp }}/Sailry-android-arm64.apk", uploads[0]["body"])
        self.assertIn("name: code-mobile-ios\n          path: ${{ runner.temp }}/Sailry-ios.ipa", uploads[1]["body"])
        self.assertIn("ipa=$RUNNER_TEMP/Sailry-ios.ipa", uploads[2]["body"])

        release = (ROOT / ".github/workflows/release.yml").read_text()
        mobile, draft = release.split("\n  mobile:\n", 1)[1].split("\n  draft:\n", 1)
        self.assertIn("    needs: version\n    uses: ./.github/workflows/mobile.yml\n", mobile)
        self.assertIn("      release_version: ${{ needs.version.outputs.version }}\n", mobile)
        self.assertIn("      release_build: ${{ needs.version.outputs.mobile_build }}\n", mobile)
        self.assertIn("    needs: [version, packages, mobile]\n", draft)
        self.assertIn("          pattern: 'code-mobile-{android,ios}'\n", draft)


if __name__ == "__main__":
    unittest.main()
