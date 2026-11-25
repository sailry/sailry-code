import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[2]


class MobileNativeTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="code native fixture ")
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name).resolve()
        self.mobile = self.root / "apps/mobile"
        for relative in ["tool/build_native.sh", "ios/build_native.sh"]:
            target = self.mobile / relative
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(ROOT / "apps/mobile" / relative, target)
        self.workspace_harness = self.root / ".runtime/harness"
        self.pub_harness = self.mobile / ".dart_tool/harness"
        for harness in [self.workspace_harness, self.pub_harness]:
            builder = harness / "apps/mobile/tool/build_native.sh"
            builder.parent.mkdir(parents=True)
            # The real entry invokes the Harness builder through sh.
            builder.write_text("#!/bin/sh\nexec \"$FIXTURE_PYTHON\" \"$0.py\" \"$@\"\n")
            builder.with_suffix(".sh.py").write_text('''import json, os, sys
from pathlib import Path
assert Path(os.environ["SAILRY_HARNESS_ROOT"]).resolve() == Path(os.environ["FIXTURE_EXPECTED_ROOT"]).resolve()
assert os.environ.get("GOWORK", "") == os.environ["FIXTURE_EXPECTED_WORKSPACE"]
assert os.environ.get("SAILRY_HARNESS_REVISION", "") == os.environ["FIXTURE_EXPECTED_REVISION"]
Path(os.environ["FIXTURE_OUTPUT"]).write_text(json.dumps(sys.argv[1:]))
raise SystemExit(int(os.environ.get("FIXTURE_STATUS", "0")))
''')

    def build_ios(self, *, prepared_workspace, status=0):
        environment = {key: value for key, value in os.environ.items()
                       if key not in {"SAILRY_HARNESS_ROOT", "SAILRY_HARNESS_REVISION", "GOWORK"}}
        revision = "0123456789abcdef0123456789abcdef01234567" if prepared_workspace else ""
        workspace = str(self.root / ".runtime/go.work") if prepared_workspace else ""
        harness = self.workspace_harness if prepared_workspace else self.pub_harness
        output = self.root / "native-arguments.json"
        environment.update({
            "SRCROOT": str(self.mobile / "ios"), "BUILT_PRODUCTS_DIR": str(self.root / "build products"),
            "PLATFORM_NAME": "iphonesimulator", "ARCHS": "arm64 x86_64", "GOWORK": workspace,
            "FIXTURE_EXPECTED_ROOT": str(harness), "FIXTURE_EXPECTED_WORKSPACE": workspace,
            "FIXTURE_EXPECTED_REVISION": revision, "FIXTURE_STATUS": str(status),
            "FIXTURE_PYTHON": sys.executable, "FIXTURE_OUTPUT": str(output),
        })
        if prepared_workspace:
            environment.update(SAILRY_HARNESS_ROOT=str(harness), SAILRY_HARNESS_REVISION=revision)
        config = (ROOT / "apps/mobile/ios/Flutter/Sailry.xcconfig").read_text()
        for key, value in re.findall(r"(?m)^(SAILRY_[A-Z_]+) = *(.*)$", config):
            environment[key] = re.sub(r"\$\(([^)]+)\)", lambda match: environment[match[1]], value)
        result = subprocess.run(["sh", str(self.mobile / "ios/build_native.sh")], cwd=self.root,
                                env=environment, capture_output=True, text=True)
        return result, json.loads(output.read_text()) if output.exists() else None

    def test_ci_native_build_uses_the_go_workspace_checkout_not_pub_cache(self):
        result, arguments = self.build_ios(prepared_workspace=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(arguments, ["ios", "iphonesimulator", "arm64", "x86_64"])

    def test_standalone_xcode_build_uses_the_pub_prepared_checkout(self):
        result, arguments = self.build_ios(prepared_workspace=False)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(arguments, ["ios", "iphonesimulator", "arm64", "x86_64"])

    def test_native_failure_is_returned_to_xcode(self):
        result, _ = self.build_ios(prepared_workspace=True, status=41)
        self.assertEqual(result.returncode, 41, result.stderr)


if __name__ == "__main__":
    unittest.main()
