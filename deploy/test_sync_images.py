import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


BASH = shutil.which('bash') or shutil.which('bash.exe')
if not BASH and Path('C:/Program Files/Git/bin/bash.exe').exists():
    BASH = 'C:/Program Files/Git/bin/bash.exe'


@unittest.skipUnless(BASH, 'Requires bash')
class ImageSyncTests(unittest.TestCase):
    def test_syncs_workers_before_reporting_success_and_removes_temporary_services(self):
        self.run_sync('success', 0)

    def test_failed_worker_stops_deployment_and_cleans_up(self):
        self.run_sync('failed', 1)

    def test_offline_node_fails_before_creating_transfer_services(self):
        self.run_sync('offline', 1)

    def test_single_node_does_not_create_transfer_services(self):
        self.run_sync('single', 0)

    def test_corrupted_archive_is_not_loaded_into_worker_docker(self):
        self.run_sync('corrupt', 1)

    def run_sync(self, scenario, expected_status):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            archive = root / 'images.tar.gz'
            archive.write_bytes(b'test archive')
            release = root / 'release.env'
            release.write_text(
                'API_IMAGE=example/api:test\nCONSTRUCTOR_IMAGE=example/constructor:test\n',
                newline='\n',
            )

            binary = root / 'bin'
            binary.mkdir()
            docker = binary / 'docker'
            docker.write_text('''#!/usr/bin/env bash
set -eu
printf '%s\\n' "$*" >> "$SYNC_AUDIT"
case "$1 $2" in
  'info --format') echo manager;;
  'node ls')
    echo 'manager Ready Active'
    if [ "$SYNC_SCENARIO" = offline ]; then echo 'worker Down Active'
    elif [ "$SYNC_SCENARIO" != single ]; then echo 'worker Ready Active'; fi;;
  'node inspect') echo amd64;;
  'node ps') echo 'honeyforge/tcp-decoy:previous Running 1 hour ago';;
  'image inspect') echo 'sha256:test';;
  'service create')
    if [ "$SYNC_SCENARIO" = corrupt ] && [[ "$*" = *hf-image-sync-* ]]; then
      payload=${!#}
      while [ "$#" -gt 0 ]; do
        if [ "$1" = --env ]; then export "$2"; shift; fi
        shift
      done
      bash -ec "$payload"
    fi
    echo created;;
  'service ps')
    case "$*" in
      *hf-image-source-*) echo 'Running 1 second ago';;
      *) if [ "$SYNC_SCENARIO" = failed ]; then echo 'Failed 1 second ago'
         else echo 'Complete 1 second ago'; fi;;
    esac;;
  'service rm'|'service logs') :;;
  *) echo "Unexpected command: $*" >&2; exit 1;;
esac
''', encoding='utf-8', newline='\n')
            docker.chmod(0o755)

            wget = binary / 'wget'
            wget.write_text('''#!/usr/bin/env bash
set -eu
while [ "$#" -gt 0 ]; do
  if [ "$1" = -O ]; then printf 'corrupted archive' > "$2"; exit 0; fi
  shift
done
exit 1
''', encoding='utf-8', newline='\n')
            wget.chmod(0o755)

            script = Path(__file__).with_name('sync-images.sh').resolve()
            # Use a temporary working directory so Git Bash receives portable relative paths.
            shutil.copyfile(script, root / 'sync-images.sh')
            audit = root / 'audit'
            environment = {
                **os.environ,
                'PATH': str(binary) + os.pathsep + os.environ['PATH'],
                'SYNC_AUDIT': str(audit),
                'SYNC_SCENARIO': scenario,
            }

            result = subprocess.run(
                [BASH, 'sync-images.sh', 'release.env', 'images.tar.gz', 'test-release'],
                cwd=root,
                env=environment,
                capture_output=True,
                text=True,
            )
            self.assertEqual(result.returncode, expected_status, result.stdout + result.stderr)

            calls = audit.read_text()
            if scenario in ('single', 'offline'):
                self.assertNotIn('service create', calls)
            else:
                self.assertIn('--constraint node.id==worker', calls)
                self.assertIn('--mount type=bind,source=/var/run/docker.sock', calls)
                self.assertIn('sha256sum -c', calls)
                self.assertIn('image inspect', calls)
                self.assertIn('service rm hf-image-sync-test-release-worker', calls)
                self.assertIn('service rm hf-image-source-test-release', calls)

            if scenario == 'corrupt':
                self.assertNotRegex(calls, r'(?m)^image load$')

            self.assertNotIn('node rm', calls)
            self.assertNotIn('swarm leave', calls)
