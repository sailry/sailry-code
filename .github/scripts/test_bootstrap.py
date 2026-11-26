import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[2]
MODULE = "github.com/sailry/sailry-harness"


class BootstrapTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.fixture = tempfile.TemporaryDirectory(prefix="code-bootstrap-source-")
        cls.source_root = Path(cls.fixture.name).resolve()
        cls.git = shutil.which("git")
        cls.git_env = {
            **os.environ, "GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": os.devnull,
            "GIT_ALLOW_PROTOCOL": "file", "GIT_TERMINAL_PROMPT": "0", "HOME": str(cls.source_root),
            "GIT_AUTHOR_DATE": "2026-11-24T01:02:03+00:00", "GIT_COMMITTER_DATE": "2026-11-24T01:02:03+00:00",
        }
        leaf = cls.repository("leaf")
        (leaf / "embedded-resource.txt").write_text("Nested pinned resource\n")
        cls.commit(leaf)
        child = cls.repository("child")
        cls.git_run("-C", str(child), "submodule", "add", "--quiet", leaf.as_uri(), "resources/nested")
        cls.commit(child)
        cls.source = cls.repository("harness")
        (cls.source / "go.mod").write_text(f"module {MODULE}\n\ngo 1.27.3\n")
        (cls.source / ".gitignore").write_text(".runtime/\n")
        (cls.source / "native/turso").mkdir(parents=True)
        (cls.source / "native/turso/build.sh").write_text('''#!/bin/sh
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
printf '%s\\n' "$root" >> "$FIXTURE_NATIVE_LOG"
test "${FIXTURE_NATIVE_FAIL:-0}" = 0
output="$root/.runtime/native/$FIXTURE_TARGET"
mkdir -p "$output"
printf '%s\\n' 'Fixture library; no native compilation' > "$output/$FIXTURE_LIBRARY"
''')
        (cls.source / "source.txt").write_text("Pinned source\n")
        for name in ["plugins", "themes"]:
            cls.git_run("-C", str(cls.source), "submodule", "add", "--quiet", child.as_uri(), name)
        cls.commit(cls.source)
        cls.pinned = cls.git_run("-C", str(cls.source), "rev-parse", "HEAD").strip()
        cls.git_run("-C", str(cls.source), "tag", "v0.1.0")
        (cls.source / "source.txt").write_text("Newer default branch\n")
        cls.commit(cls.source)
        cls.head = cls.git_run("-C", str(cls.source), "rev-parse", "HEAD").strip()

    @classmethod
    def tearDownClass(cls):
        cls.fixture.cleanup()

    @classmethod
    def git_run(cls, *arguments):
        return subprocess.check_output([cls.git, *arguments], env=cls.git_env, text=True)

    @classmethod
    def repository(cls, name):
        path = cls.source_root / name
        cls.git_run("init", "--quiet", "--initial-branch=main", str(path))
        cls.git_run("-C", str(path), "config", "user.name", "Bootstrap fixture")
        cls.git_run("-C", str(path), "config", "user.email", "fixture@example.invalid")
        return path

    @classmethod
    def commit(cls, path):
        cls.git_run("-C", str(path), "add", ".")
        cls.git_run("-C", str(path), "commit", "--quiet", "-m", "Fixture source")

    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="code-bootstrap-")
        self.addCleanup(self.temporary.cleanup)
        self.base = Path(self.temporary.name).resolve()
        self.new_code()

    def new_code(self):
        self.code = Path(tempfile.mkdtemp(prefix="product-", dir=self.base)).resolve()
        (self.code / ".github/scripts").mkdir(parents=True)
        shutil.copyfile(ROOT / "dev.sh", self.code / "dev.sh")
        shutil.copyfile(ROOT / ".github/scripts/harness-revision.sh", self.code / ".github/scripts/harness-revision.sh")
        self.manifests = {
            "go.mod": f"module github.com/sailry/sailry-code\n\ngo 1.27.3\n\nrequire {MODULE} v0.1.0\n",
            "go.sum": "Fixture Go checksum file\n",
            "apps/mobile/pubspec.yaml": "name: fixture\n",
            "apps/mobile/pubspec.lock": "Fixture Dart lockfile\n",
        }
        for name, content in self.manifests.items():
            path = self.code / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(content)
        tools = self.code / "tools"
        tools.mkdir()
        self.tool(tools / "uname", '''import os, sys
print(os.environ[{"-s": "FIXTURE_UNAME_S", "-m": "FIXTURE_UNAME_M"}[sys.argv[1]]])
''')
        self.tool(tools / "go", '''import json, os, sys
from pathlib import Path
args = sys.argv[1:]
code = Path(os.environ["FIXTURE_CODE"])
module = "github.com/sailry/sailry-harness"
manifest = (code / "go.mod").read_text()
version = manifest.split("require " + module + " ", 1)[1].split()[0]
with Path(os.environ["FIXTURE_GO_LOG"]).open("a") as log:
    log.write(json.dumps({"args": args, "cwd": os.getcwd(), "flags": os.environ.get("GOFLAGS"), "workspace": os.environ.get("GOWORK"),
                         "turso": os.environ.get("SAILRY_TURSO_LIB"), "helpers": os.environ.get("SAILRY_HELPERS_DIR"),
                         "pprof": os.environ.get("SAILRY_PPROF"),
                         "goroot": os.environ.get("GOROOT"), "toolchain": os.environ.get("GOTOOLCHAIN"),
                         "cgo": os.environ.get("CGO_ENABLED")}) + "\\n")
if args == ["mod", "edit", "-json"]:
    assert Path.cwd() == code
    metadata = {"Require": [{"Path": module, "Version": version}]}
    if "replace " + module in manifest:
        metadata["Replace"] = [{"Old": {"Path": module}, "New": {"Path": "../other-harness"}}]
    print(json.dumps(metadata))
elif args == ["mod", "download", "-json", module + "@" + version]:
    if os.environ.get("FIXTURE_OFFLINE") == "1":
        raise SystemExit("Module download disabled in offline fixture")
    assert Path.cwd() != code and not (Path.cwd() / "go.mod").exists()
    data = {"Path": module, "Version": version}
    if os.environ.get("FIXTURE_NO_ORIGIN") != "1":
        data["Origin"] = {"Hash": os.environ["FIXTURE_REVISION"]}
    print(json.dumps(data))
elif args[:2] == ["work", "init"]:
    assert Path.cwd() == code / ".runtime"
    assert not Path("go.work").exists()
    assert all((Path(path) / "go.mod").is_file() for path in args[2:])
    Path("go.work").write_text(json.dumps({"Use": [{"DiskPath": path} for path in args[2:]]}))
elif args[:3] == ["list", "-m", "-f"]:
    assert args[3] == '{{if eq .Path "github.com/sailry/sailry-harness"}}{{.Dir}}{{end}}'
    assert len(args) == 4 and os.environ["GOWORK"] == str(code / "go.work")
    print(os.environ["FIXTURE_LOCAL_HARNESS"])
elif args == ["work", "edit", "-json"]:
    print(Path(os.environ["GOWORK"]).read_text())
elif args and args[0] in {"build", "test", "vet", "run"}:
    assert os.environ["GOFLAGS"] == "-mod=readonly"
    local = os.environ.get("FIXTURE_LOCAL_HARNESS")
    assert os.environ["GOWORK"] == str(code / ("go.work" if local else ".runtime/go.work"))
    assert os.environ["SAILRY_HARNESS_DIR"] == (local or str(code / ".runtime/harness"))
else:
    raise SystemExit("Unexpected Go command in closed fixture: " + repr(args))
''')
        self.tool(tools / "git", '''import json, os, subprocess, sys
from pathlib import Path
args = sys.argv[1:]
with Path(os.environ["FIXTURE_GIT_LOG"]).open("a") as log:
    log.write(json.dumps(args) + "\\n")
if os.environ.get("FIXTURE_OFFLINE") == "1" and any(argument in {"fetch", "ls-remote"} for argument in args):
    raise SystemExit("Git network resolution disabled in offline fixture")
if "submodule" in args or any(argument.startswith("--recurse-submodules") for argument in args):
    raise SystemExit("Plugin and theme repositories are unavailable in the SDK fixture")
source = Path(os.environ["FIXTURE_SOURCE"]).as_uri()
options = ["-c", "url." + source + ".insteadOf=https://github.com/sailry/sailry-harness",
           "-c", "url." + source + ".insteadOf=https://github.com/sailry/sailry-harness.git"]
environment = {**os.environ, "PATH": os.environ["FIXTURE_REAL_PATH"]}
raise SystemExit(subprocess.run([os.environ["FIXTURE_GIT"], *options, *args], env=environment).returncode)
''')
        self.go_log, self.git_log = self.code / "go-log", self.code / "git-log"
        self.native_log = self.code / "native-log"
        self.native_log.write_text("")
        self.environment = {
            **self.git_env, "PATH": str(tools) + os.pathsep + os.environ["PATH"], "HOME": str(self.code),
            "TMPDIR": str(self.base), "FIXTURE_CODE": str(self.code), "FIXTURE_GIT": self.git,
            "FIXTURE_SOURCE": str(self.source), "FIXTURE_REVISION": self.pinned,
            "FIXTURE_GO_LOG": str(self.go_log), "FIXTURE_GIT_LOG": str(self.git_log),
            "FIXTURE_REAL_PATH": os.environ["PATH"],
            "FIXTURE_NATIVE_LOG": str(self.native_log), "FIXTURE_UNAME_S": "Darwin", "FIXTURE_UNAME_M": "arm64",
            "FIXTURE_TARGET": "darwin-arm64", "FIXTURE_LIBRARY": "libturso_sync_sdk_kit.dylib",
            "SAILRY_TURSO_LIB": "", "SAILRY_HELPERS_DIR": "",
        }
        self.environment.pop("SAILRY_PPROF", None)

    @staticmethod
    def tool(path, source):
        path.write_text(f"#!{sys.executable}\n" + source)
        path.chmod(0o755)

    def bootstrap(self, *arguments, **environment):
        self.go_log.write_text("")
        self.git_log.write_text("")
        result = subprocess.run(["bash", str(self.code / "dev.sh"), *arguments], cwd=self.base,
                                env={**self.environment, **environment}, capture_output=True, text=True)
        for name, content in self.manifests.items():
            self.assertEqual((self.code / name).read_text(), content, name)
        return result

    def assert_success(self, result):
        self.assertEqual(result.returncode, 0, result.stderr)

    def assert_untouched_checkout(self):
        calls = [json.loads(line) for line in self.git_log.read_text().splitlines()]
        self.assertFalse(any("fetch" in call or "checkout" in call or "update" in call for call in calls), calls)

    def require_version(self, version):
        content = f"module github.com/sailry/sailry-code\n\ngo 1.27.3\n\nrequire {MODULE} {version}\n"
        self.manifests["go.mod"] = content
        (self.code / "go.mod").write_text(content)

    def test_repeated_desktop_reuses_an_unpublished_managed_pseudo_version_offline(self):
        self.assert_success(self.bootstrap())
        harness = self.code / ".runtime/harness"
        self.git_run("-C", str(harness), "config", "user.name", "Local bootstrap fixture")
        self.git_run("-C", str(harness), "config", "user.email", "fixture@example.invalid")
        (harness / "source.txt").write_text("Local unpublished Harness pin\n")
        self.commit(harness)
        revision = self.git_run("-C", str(harness), "rev-parse", "HEAD").strip()
        remote = subprocess.run([self.git, "-C", str(self.source), "cat-file", "-e", revision],
                                env=self.git_env, capture_output=True, text=True)
        self.assertNotEqual(remote.returncode, 0)
        self.require_version(f"v0.1.1-0.20261124010203-{revision[:12]}")
        workspace = self.code / ".runtime/go.work"
        before = workspace.read_bytes()
        for name, arguments in [("", []), ("", []), ("dev", ["--data-dir", str(self.code / ".sailry-code-dev")]),
                                ("native-smoke", ["--data-dir", str(self.code / ".runtime/profiles/native-smoke")])]:
            with self.subTest(name=name):
                self.assert_success(self.bootstrap("desktop", name, FIXTURE_OFFLINE="1", FIXTURE_NO_ORIGIN="1",
                                                   TZ="Pacific/Honolulu"))
                self.assert_untouched_checkout()
                commands = [json.loads(line)["args"] for line in self.go_log.read_text().splitlines()]
                self.assertFalse(any(command[:2] == ["mod", "download"] for command in commands), commands)
                self.assertEqual(commands[-1], ["run", "./cmd/sailry-code-desktop", *arguments])
                self.assertEqual(workspace.read_bytes(), before)
                self.assertEqual(self.git_run("-C", str(harness), "rev-parse", "HEAD").strip(), revision)

    def test_first_pseudo_version_bootstrap_still_verifies_the_remote_pin(self):
        self.require_version(f"v0.1.1-0.20261124010203-{self.pinned[:12]}")
        self.assert_success(self.bootstrap("desktop", "native-smoke", FIXTURE_NO_ORIGIN="1"))
        commands = [json.loads(line)["args"] for line in self.go_log.read_text().splitlines()]
        self.assertIn(["mod", "download", "-json", f"{MODULE}@v0.1.1-0.20261124010203-{self.pinned[:12]}"], commands)
        calls = [json.loads(line) for line in self.git_log.read_text().splitlines()]
        self.assertTrue(any("+refs/heads/*:refs/remotes/source/*" in call for call in calls), calls)
        harness = self.code / ".runtime/harness"
        self.assertEqual(self.git_run("-C", str(harness), "rev-parse", "HEAD").strip(), self.pinned)

    def test_local_pseudo_version_prefix_or_full_timestamp_mismatch_falls_back_without_replacing_checkout(self):
        for revision, timestamp, message in [(self.head, "20261124010203", "another pin"),
                                             (self.pinned, "20261124010204", "timestamp")]:
            with self.subTest(revision=revision, timestamp=timestamp):
                self.new_code()
                self.assert_success(self.bootstrap())
                self.require_version(f"v0.1.1-0.{timestamp}-{revision[:12]}")
                workspace = self.code / ".runtime/go.work"
                before = workspace.read_bytes()
                result = self.bootstrap("desktop", FIXTURE_NO_ORIGIN="1")
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(message, result.stderr)
                commands = [json.loads(line)["args"] for line in self.go_log.read_text().splitlines()]
                self.assertTrue(any(command[:2] == ["mod", "download"] for command in commands), commands)
                calls = [json.loads(line) for line in self.git_log.read_text().splitlines()]
                self.assertTrue(any("+refs/heads/*:refs/remotes/source/*" in call for call in calls), calls)
                harness = self.code / ".runtime/harness"
                managed = [call for call in calls if call[:2] == ["-C", str(harness)]]
                self.assertFalse(any("fetch" in call or "checkout" in call or "update" in call for call in managed), managed)
                self.assertEqual(self.git_run("-C", str(harness), "rev-parse", "HEAD").strip(), self.pinned)
                self.assertEqual(workspace.read_bytes(), before)

    def test_local_pseudo_version_keeps_managed_clean_detached_and_origin_guards(self):
        for change in ["unmanaged", "branch", "origin", "dirty", "submodule"]:
            with self.subTest(change=change):
                self.new_code()
                self.assert_success(self.bootstrap())
                self.require_version(f"v0.1.1-0.20261124010203-{self.pinned[:12]}")
                harness = self.code / ".runtime/harness"
                if change == "unmanaged":
                    self.git_run("-C", str(harness), "config", "--unset", "sailry.codeBootstrap")
                elif change == "branch":
                    self.git_run("-C", str(harness), "checkout", "--quiet", "-b", "development")
                elif change == "origin":
                    self.git_run("-C", str(harness), "remote", "set-url", "origin", str(self.source))
                else:
                    if change == "submodule":
                        self.git_run("-C", str(harness), "submodule", "update", "--init", "--recursive")
                    draft = "source.txt" if change == "dirty" else "plugins/resources/nested/embedded-resource.txt"
                    (harness / draft).write_text("Preserve developer work\n")
                result = self.bootstrap("desktop", FIXTURE_OFFLINE="1")
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(self.go_log.read_text(), "")
                self.assert_untouched_checkout()

    def test_matching_local_pseudo_version_still_rejects_module_replacement(self):
        self.assert_success(self.bootstrap())
        self.require_version(f"v0.1.1-0.20261124010203-{self.pinned[:12]}")
        self.manifests["go.mod"] += f"\nreplace {MODULE} => ../other-harness\n"
        (self.code / "go.mod").write_text(self.manifests["go.mod"])
        result = self.bootstrap("desktop", FIXTURE_OFFLINE="1")
        self.assertNotEqual(result.returncode, 0)
        commands = [json.loads(line)["args"] for line in self.go_log.read_text().splitlines()]
        self.assertEqual(commands, [["mod", "edit", "-json"]])
        self.assert_untouched_checkout()

    def test_fresh_bootstrap_fetches_only_the_sdk_pin_and_runs_readonly_build(self):
        self.assert_success(self.bootstrap("build", "./cmd/sailry-code-desktop", FIXTURE_NO_ORIGIN="1"))
        harness = self.code / ".runtime/harness"
        self.assertEqual(self.git_run("-C", str(harness), "rev-parse", "HEAD").strip(), self.pinned)
        self.assertNotEqual(self.pinned, self.head)
        for name in ["plugins", "themes"]:
            self.assertFalse((harness / name / "resources/nested/embedded-resource.txt").exists())
        calls = [json.loads(line) for line in self.git_log.read_text().splitlines()]
        self.assertFalse(any("submodule" in call for call in calls), calls)
        workspace = self.code / ".runtime/go.work"
        self.assertEqual(json.loads(workspace.read_text())["Use"], [{"DiskPath": ".."}, {"DiskPath": "./harness"}])
        commands = [json.loads(line) for line in self.go_log.read_text().splitlines()]
        self.assertEqual(commands[-1]["args"], ["build", "./cmd/sailry-code-desktop"])
        before = workspace.read_bytes()
        self.assert_success(self.bootstrap())
        self.assert_untouched_checkout()
        self.assertEqual(workspace.read_bytes(), before)

    def test_workspace_accepts_portable_and_older_absolute_module_paths(self):
        self.assert_success(self.bootstrap())
        workspace = self.code / '.runtime/go.work'
        for paths in [['..', './harness'], [str(self.code), str(self.code / '.runtime/harness')]]:
            with self.subTest(paths=paths):
                original = json.dumps({'Use': [{'DiskPath': path} for path in paths]})
                workspace.write_text(original)
                self.assert_success(self.bootstrap('vet', './...'))
                self.assertEqual(workspace.read_text(), original)
                self.assert_untouched_checkout()

    def test_existing_development_checkout_is_not_adopted_or_modified(self):
        harness = self.code / ".runtime/harness"
        harness.parent.mkdir()
        self.git_run("clone", "--quiet", str(self.source), str(harness))
        sentinel = harness / "developer-draft.txt"
        sentinel.write_text("Preserve developer work\n")
        result = self.bootstrap()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("not a managed", result.stderr)
        self.assert_untouched_checkout()
        self.assertEqual(sentinel.read_text(), "Preserve developer work\n")
        self.assertEqual(self.git_run("-C", str(harness), "rev-parse", "HEAD").strip(), self.head)
        self.assertEqual(self.go_log.read_text(), "")

    def test_dirty_root_and_nested_submodule_are_preserved(self):
        for relative in ["source.txt", "plugins/resources/nested/embedded-resource.txt"]:
            with self.subTest(relative=relative):
                self.new_code()
                self.assert_success(self.bootstrap())
                if relative.startswith("plugins/"):
                    self.git_run("-C", str(self.code / ".runtime/harness"), "submodule", "update", "--init", "--recursive")
                draft = self.code / ".runtime/harness" / relative
                draft.write_text("Uncommitted developer edit\n")
                result = self.bootstrap()
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("submodules changed", result.stderr)
                self.assert_untouched_checkout()
                self.assertEqual(draft.read_text(), "Uncommitted developer edit\n")

    def test_changed_pin_is_rejected_without_fetching_or_replacing_checkout(self):
        self.assert_success(self.bootstrap())
        result = self.bootstrap(FIXTURE_REVISION=self.head)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("another pin", result.stderr)
        self.assert_untouched_checkout()
        self.assertEqual(self.git_run("-C", str(self.code / ".runtime/harness"), "rev-parse", "HEAD").strip(), self.pinned)

    def test_custom_workspace_is_not_overwritten(self):
        workspace = self.code / ".runtime/go.work"
        workspace.parent.mkdir()
        original = json.dumps({"Use": [{"DiskPath": str(self.base / "developer")}]})
        workspace.write_text(original)
        result = self.bootstrap()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("custom workspace", result.stderr)
        self.assertEqual(workspace.read_text(), original)
        self.assertFalse((self.code / ".runtime/harness").exists())
        self.assert_untouched_checkout()

    def test_development_branch_or_changed_origin_is_not_modified(self):
        for change in ["branch", "origin"]:
            with self.subTest(change=change):
                self.new_code()
                self.assert_success(self.bootstrap())
                harness = self.code / ".runtime/harness"
                if change == "branch":
                    self.git_run("-C", str(harness), "checkout", "--quiet", "-b", "development")
                else:
                    self.git_run("-C", str(harness), "remote", "set-url", "origin", str(self.source))
                result = self.bootstrap()
                self.assertNotEqual(result.returncode, 0)
                self.assert_untouched_checkout()
                self.assertEqual(self.go_log.read_text(), "")

    def test_symlinked_bootstrap_paths_are_rejected(self):
        for name in [".runtime", ".runtime/harness", ".runtime/go.work", ".runtime/go.work.sum"]:
            with self.subTest(name=name):
                self.new_code()
                target = self.code / name
                target.parent.mkdir(parents=True, exist_ok=True)
                target.symlink_to(self.code / "go.sum")
                result = self.bootstrap()
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("must not be symlinks", result.stderr)
                self.assertEqual(self.go_log.read_text(), "")

    def test_help_and_unsupported_mutating_commands_do_not_prepare_anything(self):
        result = self.bootstrap("--help")
        self.assert_success(result)
        self.assertGreaterEqual(len(result.stdout.splitlines()), 15)
        self.assertLessEqual(len(result.stdout.splitlines()), 25)
        self.assertIn("no --data-dir", result.stdout)
        self.assertIn("Code preinstalls no plugins or themes", result.stdout)
        self.assertIn("just desktop native-smoke twice", result.stdout)
        for arguments in [("mod", "tidy"), ("build", "-mod=mod"), ("test", "-modfile=other.mod")]:
            with self.subTest(arguments=arguments):
                result = self.bootstrap(*arguments)
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse((self.code / ".runtime").exists())
                self.assertEqual(self.go_log.read_text(), "")

    def test_development_commands_use_the_local_harness_and_its_native_library(self):
        local = self.base / "local Harness"
        shutil.copytree(self.source / "native", local / "native")
        workspace = self.code / "go.work"
        workspace.write_text(f'go 1.27.3\nuse (\n.\n"{local}"\n)\n')
        env = {"FIXTURE_OFFLINE": "1", "FIXTURE_LOCAL_HARNESS": str(local)}
        for arguments in [("build", "./..."), ("test", "./..."), ("vet", "./..."), ("desktop",)]:
            with self.subTest(arguments=arguments):
                self.assert_success(self.bootstrap(*arguments, **env))
                calls = [json.loads(line) for line in self.go_log.read_text().splitlines()]
                self.assertEqual([call["args"][0] for call in calls], ["list", "run" if arguments == ("desktop",) else arguments[0]])
                self.assertEqual(self.git_log.read_text(), "")
        desktop = calls[-1]
        self.assertEqual(desktop["args"], ["run", "./cmd/sailry-code-desktop"])
        native = local / ".runtime/native/darwin-arm64"
        self.assertEqual(desktop["turso"], str(native / "libturso_sync_sdk_kit.dylib"))
        self.assertEqual(desktop["helpers"], str(native / "helpers"))
        self.assertFalse((self.code / ".runtime/harness").exists())
        self.assertEqual(self.native_log.read_text().splitlines(), [str(local)])

    def test_desktop_default_uses_the_code_product_entry_without_a_data_dir(self):
        self.assert_success(self.bootstrap("desktop", GOROOT="inherited-wrong-root"))
        commands = [json.loads(line) for line in self.go_log.read_text().splitlines()]
        desktop = commands[-1]
        native = self.code / ".runtime/harness/.runtime/native/darwin-arm64"
        self.assertEqual(desktop["args"], ["run", "./cmd/sailry-code-desktop"])
        self.assertEqual(desktop["cwd"], str(self.code))
        self.assertEqual(desktop["turso"], str(native / "libturso_sync_sdk_kit.dylib"))
        self.assertEqual(desktop["helpers"], str(native / "helpers"))
        self.assertEqual(desktop["pprof"], "127.0.0.1:6061")
        self.assertIsNone(desktop["goroot"])
        self.assertEqual(desktop["toolchain"], "go1.27.3")
        self.assertEqual(desktop["cgo"], "0")
        self.assertFalse((self.code / ".runtime/profiles").exists())

    def test_desktop_pprof_defaults_and_allows_override_or_disable(self):
        self.assert_success(self.bootstrap("desktop", "native-smoke", SAILRY_PPROF="127.0.0.1:6062"))
        overridden = json.loads(self.go_log.read_text().splitlines()[-1])
        self.assertEqual(overridden["pprof"], "127.0.0.1:6062")
        self.assert_success(self.bootstrap("desktop", "native-smoke", SAILRY_PPROF=""))
        disabled = json.loads(self.go_log.read_text().splitlines()[-1])
        self.assertEqual(disabled["pprof"], "")

    def test_desktop_dev_and_named_profiles_are_independent_of_harness_profiles(self):
        for name, path in [("dev", self.code / ".sailry-code-dev"),
                           ("ui-smoke", self.code / ".runtime/profiles/ui-smoke")]:
            with self.subTest(name=name):
                self.assert_success(self.bootstrap("desktop", name))
                desktop = json.loads(self.go_log.read_text().splitlines()[-1])
                self.assertEqual(desktop["args"], ["run", "./cmd/sailry-code-desktop", "--data-dir", str(path)])
        self.assertEqual(len(self.native_log.read_text().splitlines()), 1)

    def test_invalid_profile_names_and_native_options_fail_before_bootstrap(self):
        for arguments in [("desktop", "."), ("desktop", ".."), ("desktop", "../../.sailry"),
                          ("desktop", "/tmp/profile"), ("desktop", "nested/profile"),
                          ("desktop", r"nested\profile"), ("desktop", "profile;touch injected"),
                          ("desktop", "name", "extra"), ("native", "extra"), ("native-build", "extra")]:
            with self.subTest(arguments=arguments):
                result = self.bootstrap(*arguments)
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse((self.code / ".runtime").exists())
                self.assertEqual(self.go_log.read_text(), "")

    def test_native_reuses_the_managed_build_and_explicit_rebuild_is_local(self):
        self.assert_success(self.bootstrap("native"))
        native = self.code / ".runtime/harness/.runtime/native/darwin-arm64/libturso_sync_sdk_kit.dylib"
        self.assertTrue(native.is_file())
        reused = self.bootstrap("native")
        self.assert_success(reused)
        self.assertIn(str(native), reused.stderr)
        self.assertEqual(len(self.native_log.read_text().splitlines()), 1)
        self.assert_success(self.bootstrap("native-build"))
        self.assertEqual(self.native_log.read_text().splitlines(), [str(self.code / ".runtime/harness")] * 2)
        commands = [json.loads(line) for line in self.go_log.read_text().splitlines()]
        self.assertFalse(any(command["args"][0] == "run" for command in commands))
        self.assertFalse((self.code / ".runtime/native").exists())

    def test_explicit_native_library_skips_build_and_missing_file_does_not_launch(self):
        native = self.base / "external runtime/libturso_sync_sdk_kit.dylib"
        native.parent.mkdir()
        native.write_text("Explicit native fixture\n")
        helpers = self.base / "explicit helpers"
        self.assert_success(self.bootstrap("desktop", "native-smoke", SAILRY_TURSO_LIB=str(native),
                                           SAILRY_HELPERS_DIR=str(helpers)))
        desktop = json.loads(self.go_log.read_text().splitlines()[-1])
        self.assertEqual(desktop["turso"], str(native))
        self.assertEqual(desktop["helpers"], str(helpers))
        self.assertEqual(self.native_log.read_text(), "")
        result = self.bootstrap("desktop", SAILRY_TURSO_LIB=str(native.parent / "missing.dylib"))
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("SAILRY_TURSO_LIB must name an existing", result.stderr)
        self.assertEqual(self.native_log.read_text(), "")
        commands = [json.loads(line) for line in self.go_log.read_text().splitlines()]
        self.assertFalse(any(command["args"][0] == "run" for command in commands))

    def test_failed_native_build_does_not_launch_desktop(self):
        result = self.bootstrap("desktop", FIXTURE_NATIVE_FAIL="1")
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(len(self.native_log.read_text().splitlines()), 1)
        commands = [json.loads(line) for line in self.go_log.read_text().splitlines()]
        self.assertFalse(any(command["args"][0] == "run" for command in commands))

    def test_native_platform_mapping_matches_pinned_builder(self):
        for system, machine, target, library in [
            ("Darwin", "x86_64", "darwin-amd64", "libturso_sync_sdk_kit.dylib"),
            ("Linux", "aarch64", "linux-arm64", "libturso_sync_sdk_kit.so"),
            ("Linux", "amd64", "linux-amd64", "libturso_sync_sdk_kit.so"),
            ("MINGW64_NT-10.0", "x86_64", "windows-amd64", "turso_sync_sdk_kit.dll"),
        ]:
            with self.subTest(target=target):
                self.new_code()
                self.assert_success(self.bootstrap("desktop", "native-smoke", FIXTURE_UNAME_S=system,
                                                   FIXTURE_UNAME_M=machine, FIXTURE_TARGET=target,
                                                   FIXTURE_LIBRARY=library))
                desktop = json.loads(self.go_log.read_text().splitlines()[-1])
                self.assertEqual(desktop["turso"], str(self.code / ".runtime/harness/.runtime/native" / target / library))

    def test_just_recipes_route_to_code_without_interpolating_profile_names(self):
        recipes = (ROOT / "justfile").read_text()
        self.assertIn('[positional-arguments]\ndesktop name="":\n    bash dev.sh desktop "$1"\n', recipes)
        for recipe in ["native", "native-build"]:
            self.assertIn(f"{recipe}:\n    bash dev.sh {recipe}\n", recipes)
        self.assertNotIn("import ", recipes)
        self.assertNotIn("./cmd/sailry-desktop", (ROOT / "dev.sh").read_text())

    def test_ci_and_local_entry_use_the_same_bootstrap_and_pin_resolver(self):
        action = (ROOT / ".github/actions/harness/action.yml").read_text()
        entry = (ROOT / "dev.sh").read_text()
        self.assertIn("        bash dev.sh\n", action)
        self.assertIn('revision=$(bash .github/scripts/harness-revision.sh "$verified_revision" "$verified_timestamp")', entry)
        self.assertNotIn("actions/checkout@", action)


if __name__ == "__main__":
    unittest.main()
