import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


@unittest.skipUnless(shutil.which('bash'), 'Requires bash (Linux CI)')
class ReleaseCleanupTests(unittest.TestCase):
    def test_missing_release_directory_fails_before_cleanup(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            data = root / 'database'
            data.write_bytes(b'do not touch')
            script = Path(__file__).with_name('cleanup-release-artifacts.sh')
            result = subprocess.run(
                ['bash', str(script)],
                env={**os.environ, 'DEPLOY_ROOT': str(root)},
                capture_output=True,
                text=True,
            )
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(data.read_bytes(), b'do not touch')

    def test_cleanup_preserves_live_rollback_images_and_data(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for name in ('100-1', '101-1', '102-1', 'manual-files'):
                release = root / 'incoming' / name
                release.mkdir(parents=True)
                (release / 'images.tar.gz').write_bytes(b'archive')
            (root / 'current').symlink_to(root / 'incoming' / '102-1', target_is_directory=True)
            (root / 'current' / 'release.env').write_text('API_IMAGE=honeyforge/api:rollback\n')
            (root / 'database').write_bytes(b'do not touch')
            binary = root / 'bin'
            binary.mkdir()
            docker = binary / 'docker'
            docker.write_text('''#!/usr/bin/env python3
import os, sys
args = sys.argv[1:]
if args[:2] == ['info', '--format']: print(os.environ['DEPLOY_ROOT'])
elif args[:2] == ['ps', '-a']: print('honeyforge/api:live')
elif args[:2] == ['service', 'ls']: print('honeyforge/tcp-decoy:pending@sha256:d')
elif args[:2] == ['image', 'inspect']:
    print({'honeyforge/api:live':'sha256:live', 'honeyforge/api:rollback':'sha256:rollback', 'honeyforge/tcp-decoy:pending':'sha256:pending'}.get(args[-1], 'sha256:missing'))
elif args[:2] == ['image', 'ls']:
    print('honeyforge/api live sha256:live\\nhoneyforge/api rollback sha256:rollback\\nhoneyforge/tcp-decoy pending sha256:pending\\nhoneyforge/constructor old sha256:old\\nunrelated/data old sha256:unrelated')
elif args[:2] == ['image', 'rm']:
    with open(os.environ['CLEANUP_AUDIT'], 'a') as output: output.write(args[-1]+'\\n')
else: raise SystemExit('Unexpected Docker operation: '+str(args))
''')
            docker.chmod(0o755)
            audit = root / 'audit'
            env = {**os.environ, 'PATH': str(binary) + os.pathsep + os.environ['PATH'], 'DEPLOY_ROOT': str(root), 'CLEANUP_AUDIT': str(audit)}
            script = Path(__file__).with_name('cleanup-release-artifacts.sh')
            result = subprocess.run(['bash', str(script)], env=env, capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertFalse((root / 'incoming' / '100-1' / 'images.tar.gz').exists())
            self.assertFalse((root / 'incoming' / '101-1' / 'images.tar.gz').exists())
            self.assertTrue((root / 'current' / 'images.tar.gz').exists())
            self.assertTrue((root / 'incoming' / 'manual-files' / 'images.tar.gz').exists())
            self.assertEqual((root / 'database').read_bytes(), b'do not touch')
            self.assertEqual(audit.read_text().splitlines(), ['honeyforge/constructor:old'])
