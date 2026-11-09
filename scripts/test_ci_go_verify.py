"""Inject failures into each Go stage without running or changing repository code."""
import os
from pathlib import Path
import subprocess
import tempfile

helper = Path(__file__).with_name("ci-go-verify.sh").resolve()
with tempfile.TemporaryDirectory() as temporary:
    root = Path(temporary)
    (root / "gofmt").write_text("#!/bin/sh\nexit 0\n")
    (root / "gofmt").chmod(0o755)
    (root / "go").write_text('''#!/bin/sh
n=0
[ ! -f "$COUNT" ] || n=$(cat "$COUNT")
n=$((n + 1))
printf '%s' "$n" > "$COUNT"
[ "$n" -ne "$FAIL_AT" ] || exit 42
exit 0
''')
    (root / "go").chmod(0o755)
    for stage in range(1, 7):
        count = root / "count"
        count.unlink(missing_ok=True)
        env = dict(os.environ, PATH=str(root) + os.pathsep + os.environ["PATH"],
                   COUNT=str(count), FAIL_AT=str(stage), RUNNER_TEMP=str(root))
        run = subprocess.run(["sh", str(helper)], cwd=root, env=env, check=False)
        assert run.returncode == 42, (stage, run.returncode)
        assert int(count.read_text()) == stage, stage
print("PASS: six injected failures propagate unchanged and stop later stages")
