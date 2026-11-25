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
        (cls.source / "source.txt").write_text("Pinned source\n")
        cls.git_run("-C", str(cls.source), "submodule", "add", "--quiet", child.as_uri(), "packages/official")
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
        self.tool(tools / "go", '''import json, os, sys
from pathlib import Path
args = sys.argv[1:]
code = Path(os.environ["FIXTURE_CODE"])
with Path(os.environ["FIXTURE_GO_LOG"]).open("a") as log:
    log.write(json.dumps({"args": args, "cwd": os.getcwd(), "flags": os.environ.get("GOFLAGS"), "workspace": os.environ.get("GOWORK")}) + "\\n")
if args == ["mod", "edit", "-json"]:
    assert Path.cwd() == code
    print(json.dumps({"Require": [{"Path": "github.com/sailry/sailry-harness", "Version": "v0.1.0"}]}))
elif args == ["mod", "download", "-json", "github.com/sailry/sailry-harness@v0.1.0"]:
    assert Path.cwd() != code and not (Path.cwd() / "go.mod").exists()
    data = {"Path": "github.com/sailry/sailry-harness", "Version": "v0.1.0"}
    if os.environ.get("FIXTURE_NO_ORIGIN") != "1":
        data["Origin"] = {"Hash": os.environ["FIXTURE_REVISION"]}
    print(json.dumps(data))
elif args[:2] == ["work", "init"]:
    assert Path.cwd() == code / ".runtime"
    assert not Path("go.work").exists()
    assert all((Path(path) / "go.mod").is_file() for path in args[2:])
    Path("go.work").write_text(json.dumps({"Use": [{"DiskPath": path} for path in args[2:]]}))
elif args == ["work", "edit", "-json"]:
    print(Path(os.environ["GOWORK"]).read_text())
elif args and args[0] in {"build", "test", "vet"}:
    assert os.environ["GOFLAGS"] == "-mod=readonly"
    assert os.environ["GOWORK"] == str(code / ".runtime/go.work")
    assert os.environ["SAILRY_HARNESS_DIR"] == str(code / ".runtime/harness")
else:
    raise SystemExit("Unexpected Go command in closed fixture: " + repr(args))
''')
        self.tool(tools / "git", '''import json, os, subprocess, sys
from pathlib import Path
args = sys.argv[1:]
with Path(os.environ["FIXTURE_GIT_LOG"]).open("a") as log:
    log.write(json.dumps(args) + "\\n")
source = Path(os.environ["FIXTURE_SOURCE"]).as_uri()
options = ["-c", "url." + source + ".insteadOf=https://github.com/sailry/sailry-harness",
           "-c", "url." + source + ".insteadOf=https://github.com/sailry/sailry-harness.git"]
raise SystemExit(subprocess.run([os.environ["FIXTURE_GIT"], *options, *args]).returncode)
''')
        self.go_log, self.git_log = self.code / "go-log", self.code / "git-log"
        self.environment = {
            **self.git_env, "PATH": str(tools) + os.pathsep + os.environ["PATH"], "HOME": str(self.code),
            "TMPDIR": str(self.base), "FIXTURE_CODE": str(self.code), "FIXTURE_GIT": self.git,
            "FIXTURE_SOURCE": str(self.source), "FIXTURE_REVISION": self.pinned,
            "FIXTURE_GO_LOG": str(self.go_log), "FIXTURE_GIT_LOG": str(self.git_log),
        }

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

    def test_fresh_bootstrap_fetches_pinned_recursive_resources_and_runs_readonly_build(self):
        self.assert_success(self.bootstrap("build", "./cmd/sailry-code-desktop", FIXTURE_NO_ORIGIN="1"))
        harness = self.code / ".runtime/harness"
        self.assertEqual(self.git_run("-C", str(harness), "rev-parse", "HEAD").strip(), self.pinned)
        self.assertNotEqual(self.pinned, self.head)
        self.assertEqual((harness / "packages/official/resources/nested/embedded-resource.txt").read_text(), "Nested pinned resource\n")
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
        for relative in ["source.txt", "packages/official/resources/nested/embedded-resource.txt"]:
            with self.subTest(relative=relative):
                self.new_code()
                self.assert_success(self.bootstrap())
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
        self.assert_success(self.bootstrap("--help"))
        for arguments in [("mod", "tidy"), ("build", "-mod=mod"), ("test", "-modfile=other.mod")]:
            with self.subTest(arguments=arguments):
                result = self.bootstrap(*arguments)
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse((self.code / ".runtime").exists())
                self.assertEqual(self.go_log.read_text(), "")

    def test_ci_and_local_entry_use_the_same_bootstrap_and_pin_resolver(self):
        action = (ROOT / ".github/actions/harness/action.yml").read_text()
        entry = (ROOT / "dev.sh").read_text()
        self.assertIn("        bash dev.sh\n", action)
        self.assertIn("revision=$(bash .github/scripts/harness-revision.sh)", entry)
        self.assertNotIn("actions/checkout@", action)


if __name__ == "__main__":
    unittest.main()
