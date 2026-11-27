from datetime import datetime
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[2]
REPO = "sailry/sailry-code"
DESKTOP = "cmd/sailry-code-desktop/mygo.json"
TAG = "v0.3.3"
PUSH_TIME = "2026-11-26T01:00:00Z"
PUSH_EPOCH = int(datetime.fromisoformat(PUSH_TIME.replace("Z", "+00:00")).timestamp())


class ScopeTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix="code-ci-scope-")
        self.addCleanup(temporary.cleanup)
        root = Path(temporary.name)
        self.repo = root / "source"
        self.repo.mkdir()
        tools = root / "tools"
        tools.mkdir()
        self.output, self.event = root / "output", root / "event.json"
        self.response, self.requests = root / "response.json", root / "requests"
        curl = tools / "curl"
        curl.write_text(f"#!{sys.executable}\n" + r'''import os, sys
from pathlib import Path
Path(os.environ["FIXTURE_REQUESTS"]).write_text("\n".join(sys.argv[1:]))
response = Path(os.environ["FIXTURE_RESPONSE"])
if not response.exists():
    raise SystemExit(22)
sys.stdout.write(response.read_text())
''')
        curl.chmod(0o755)
        self.env = {
            "HOME": str(root), "PATH": str(tools) + os.pathsep + os.environ["PATH"],
            "GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": os.devnull,
            "GIT_ALLOW_PROTOCOL": "file", "GIT_TERMINAL_PROMPT": "0",
            "GITHUB_API_URL": "https://api.github.invalid", "GITHUB_REPOSITORY": REPO,
            "GITHUB_OUTPUT": str(self.output), "GITHUB_EVENT_PATH": str(self.event),
            "FIXTURE_RESPONSE": str(self.response), "FIXTURE_REQUESTS": str(self.requests),
        }
        self.git("init", "--quiet", "--initial-branch=main")
        self.git("config", "user.name", "Workflow fixture")
        self.git("config", "user.email", "fixture@example.invalid")
        self.base = self.commit(DESKTOP, '{"version":"0.3.2"}')
        self.git("tag", "v0.3.2")

    def git(self, *args):
        return subprocess.check_output(["git", *args], cwd=self.repo, env=self.env, text=True).strip()

    def commit(self, path, content):
        path = self.repo / path
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content)
        self.git("add", "--", str(path))
        self.git("commit", "--quiet", "-m", "Fixture " + path.name)
        return self.git("rev-parse", "HEAD")

    def candidate(self):
        self.commit(DESKTOP, '{"version":"0.3.3"}')
        self.commit("modules/code/file.go", "candidate code\n")
        return self.commit("README.md", "candidate docs\n")

    def release_run(self):
        sha = self.git("rev-parse", "HEAD")
        return {
            "id": 42, "path": ".github/workflows/release.yml", "head_branch": TAG, "head_sha": sha,
            "event": "push", "status": "completed", "conclusion": "success",
            "created_at": "2026-11-26T00:20:00Z", "updated_at": "2026-11-26T00:59:59Z",
            "repository": {"full_name": REPO}, "head_repository": {"full_name": REPO},
            "referenced_workflows": [
                {"ref": f"refs/tags/{TAG}", "sha": sha, "path": f"{REPO}/.github/workflows/ci.yml@{sha}"},
            ],
        }

    def api_runs(self, *runs):
        self.response.write_text(json.dumps({"total_count": len(runs), "workflow_runs": runs}))

    def run_scope(self, event=None, **overrides):
        self.output.unlink(missing_ok=True)
        self.requests.unlink(missing_ok=True)
        env = {
            **self.env, "GITHUB_EVENT_NAME": "push", "GITHUB_REF": "refs/heads/main",
            "BEFORE": self.base, "GITHUB_SHA": self.git("rev-parse", "HEAD"), **overrides,
        }
        self.event.write_text(json.dumps({
            "ref": env["GITHUB_REF"], "before": env["BEFORE"], "after": env["GITHUB_SHA"],
            "repository": {"full_name": REPO, "pushed_at": PUSH_EPOCH},
            **(event or {}),
        }))
        return subprocess.run(["bash", str(ROOT / ".github/scripts/ci-scope.sh")], cwd=self.repo,
                              env=env, capture_output=True, text=True)

    def assert_scope(self, result, go, packages):
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.output.read_text(), f"go={go}\npackages={packages}\n")

    def assert_failed(self, result, message):
        self.assertNotEqual(result.returncode, 0, result.stdout)
        self.assertIn(message, result.stderr)
        self.assertFalse(self.output.exists())

    def test_successful_tag_promotion_uses_full_push_range_and_peels_tags(self):
        sha = self.candidate()
        run = self.release_run()
        self.api_runs({**run, "head_branch": "main"}, run)
        for annotated in (False, True):
            with self.subTest(annotated=annotated):
                if annotated:
                    self.git("tag", "--delete", TAG)
                    self.git("tag", "-a", TAG, "-m", "Annotated fixture")
                    self.assertNotEqual(self.git("rev-parse", f"refs/tags/{TAG}"), sha)
                else:
                    self.git("tag", TAG)
                event = {"repository": {"full_name": REPO, "pushed_at": PUSH_TIME}} if annotated else None
                result = self.run_scope(event=event)
                self.assert_scope(result, "false", "false")
                self.assertIn(f"Reusing Code Release run 42 for {TAG} at {sha}", result.stdout)
                self.assertEqual(self.requests.read_text().splitlines(), [
                    "-fsS", "-H", "Accept: application/vnd.github+json",
                    f"https://api.github.invalid/repos/{REPO}/actions/workflows/release.yml/runs"
                    f"?event=push&branch={TAG}&head_sha={sha}&status=success&per_page=100",
                ])

    def test_main_first_or_wrong_tag_fails_without_api_lookup(self):
        self.candidate()
        self.api_runs(self.release_run())
        # A tag-like branch and neighboring tags cannot substitute for the exact tag.
        self.git("branch", TAG)
        self.git("tag", TAG + "-rc.1")
        self.assert_failed(self.run_scope(), "Use tag-first")
        self.assertFalse(self.requests.exists())
        self.git("tag", TAG, self.base)
        self.assert_failed(self.run_scope(), "does not resolve")
        self.assertFalse(self.requests.exists())

    def test_nonmatching_or_late_release_runs_fail_closed(self):
        self.candidate()
        self.git("tag", TAG)
        run = self.release_run()
        reference = run["referenced_workflows"][0]
        for overrides in [
            {"repository": {"full_name": "fork/sailry-code"}}, {"head_repository": {"full_name": "fork/sailry-code"}},
            {"head_branch": TAG + "-rc.1"}, {"head_sha": self.base}, {"event": "workflow_dispatch"},
            {"path": ".github/workflows/mobile.yml"}, {"status": "in_progress"}, {"conclusion": "failure"},
            {"referenced_workflows": []}, {"referenced_workflows": [{**reference, "ref": f"refs/heads/{TAG}"}]},
            {"referenced_workflows": [{**reference, "sha": self.base}]},
            {"referenced_workflows": [{**reference, "path": reference["path"].replace("ci.yml", "other.yml")}]},
            {"updated_at": None}, {"updated_at": PUSH_TIME}, {"updated_at": "2026-11-26T01:01:00Z"},
        ]:
            with self.subTest(overrides=overrides):
                self.api_runs({**run, **overrides})
                result = self.run_scope()
                self.assert_failed(result, "No verified Code Release")
                self.assertIn("Use tag-first", result.stderr)
        self.api_runs()
        self.assert_failed(self.run_scope(), "No verified Code Release")
        self.response.unlink()
        self.assert_failed(self.run_scope(), "No verified Code Release")

    def test_unverified_push_event_cannot_reuse_release(self):
        self.candidate()
        self.git("tag", TAG)
        self.api_runs(self.release_run())
        for event in [{"repository": {"full_name": REPO}}, {"ref": f"refs/tags/{TAG}"},
                      {"after": self.base}, {"repository": {"full_name": "fork/sailry-code", "pushed_at": PUSH_TIME}}]:
            with self.subTest(event=event):
                self.assert_failed(self.run_scope(event=event), "Cannot verify the main push timestamp")
                self.assertFalse(self.requests.exists())

    def test_missing_inaccessible_or_non_fast_forward_range_fails_closed(self):
        self.commit("README.md", "docs\n")
        descendant = self.commit("docs/guide.md", "later docs\n")
        self.git("checkout", "--quiet", "HEAD^1")
        for before in ("", "0" * 40, "a" * 40, "HEAD^1", descendant):
            with self.subTest(before=before):
                self.assert_failed(self.run_scope(BEFORE=before), "Cannot verify the main push range")
        self.assert_failed(self.run_scope(GITHUB_SHA=descendant), "Cannot verify the main push range")
        self.assertFalse(self.requests.exists())

    def test_unreadable_desktop_metadata_fails_closed(self):
        bad = self.commit(DESKTOP, '{"version":"0.3.3"} {}')
        self.assert_failed(self.run_scope(), "Cannot read the pushed Desktop version")
        self.commit(DESKTOP, '{"version":"0.3.3"}')
        self.assert_failed(self.run_scope(BEFORE=bad), "Cannot read the previous main Desktop version")

    def test_ordinary_main_and_pull_request_lanes_are_unchanged(self):
        self.commit("docs/guide.md", "docs\n")
        self.assert_scope(self.run_scope(), "false", "false")
        self.assert_scope(self.run_scope(GITHUB_EVENT_NAME="pull_request"), "false", "false")
        self.commit("modules/code/ssh/service/service.go", "module\n")
        self.assert_scope(self.run_scope(), "true", "true")
        self.assert_scope(self.run_scope(GITHUB_EVENT_NAME="pull_request"), "true", "false")
        for path in ("modules/code/git/service/executable_windows.go", "modules/code/terminal/node/service.go",
                     "cmd/sailry-code-desktop/main.go", ".github/scripts/ci-scope.sh"):
            with self.subTest(path=path):
                self.commit(path, "packaging input\n")
                self.assert_scope(self.run_scope(GITHUB_EVENT_NAME="pull_request"), "true", "true")
        before = self.git("rev-parse", "HEAD")
        self.commit(DESKTOP, '{"version":"0.3.2","name":"Fixture"}')
        self.assert_scope(self.run_scope(BEFORE=before), "true", "true")
        self.assertFalse(self.requests.exists())

    def test_manual_ci_and_tag_workflow_call_keep_the_full_matrix(self):
        self.commit("README.md", "docs\n")
        for settings in [
            {"GITHUB_EVENT_NAME": "workflow_dispatch"},
            {"GITHUB_EVENT_NAME": "workflow_dispatch", "INTEGRATION": "true"},
            {"GITHUB_EVENT_NAME": "push", "GITHUB_REF": f"refs/tags/{TAG}", "DISTRIBUTION": "true"},
        ]:
            with self.subTest(settings=settings):
                self.assert_scope(self.run_scope(BEFORE="", **settings), "true", "true")
        self.assertFalse(self.requests.exists())

    def test_workflow_keeps_the_scope_as_the_lightweight_gate(self):
        ci = (ROOT / ".github/workflows/ci.yml").read_text()
        trigger = ci.split("\n  push:\n", 1)[1].split("\n  workflow_dispatch:\n", 1)[0]
        self.assertEqual(trigger, "    branches: [main]")
        scope = ci.split("\n  scope:\n", 1)[1].split("\n  native-linux:\n", 1)[0]
        self.assertIn("BEFORE: ${{ github.event.before }}", scope)
        self.assertIn("github.ref == 'refs/heads/main' && '0'", scope)
        self.assertIn("run: bash .github/scripts/ci-scope.sh", scope)
        for lane, output in [("native-linux", "go"), ("checks", "go"), ("native", "packages"),
                             ("platform-tests", "packages"), ("macos", "packages"), ("windows", "packages"),
                             ("host-linux", "packages")]:
            with self.subTest(lane=lane):
                job = re.split(r"\n(?=  [^ ])", ci.split(f"\n  {lane}:\n", 1)[1], maxsplit=1)[0]
                self.assertIn(f"needs.scope.outputs.{output} == 'true'", job)


if __name__ == "__main__":
    unittest.main()
