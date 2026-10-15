import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import textwrap
import unittest


ROOT = Path(__file__).resolve().parent.parent
BASH = shutil.which('bash') or shutil.which('bash.exe')
if os.name == 'nt' and Path('C:/Program Files/Git/bin/bash.exe').exists():
    BASH = 'C:/Program Files/Git/bin/bash.exe'


class NativeReleaseTests(unittest.TestCase):
    @unittest.skipUnless(BASH, 'Requires bash')
    def test_release_packages_native_images_and_production_override(self):
        # Run the real release packaging step with a recording Docker command.
        # No Docker daemon or deployment credentials are needed for this test.
        workflow = (ROOT / '.github/workflows/ci-cd.yml').read_text(encoding='utf-8')
        step = workflow.split('- name: Build and package versioned Docker images\n', 1)[1]
        script = textwrap.dedent(step.split('        run: |\n', 1)[1].split('\n      - ', 1)[0])
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            shutil.copytree(ROOT / 'deploy', root / 'deploy', ignore=shutil.ignore_patterns('__pycache__'))
            (root / 'frontend/dist').mkdir(parents=True)
            (root / 'frontend/dist/index.html').write_text('release fixture', encoding='utf-8')
            binary = root / 'bin'
            binary.mkdir()
            docker = binary / 'docker'
            docker.write_text('#!/bin/sh\nprintf "%s\\n" "$*" >> "$DOCKER_AUDIT"\n', encoding='utf-8', newline='\n')
            docker.chmod(0o755)
            environment = dict(os.environ, PATH=str(binary) + os.pathsep + os.environ['PATH'],
                               GITHUB_SHA='testsha', GITHUB_RUN_ID='123', GITHUB_RUN_ATTEMPT='1',
                               DOCKER_AUDIT=str(root / 'docker.log'))
            result = subprocess.run([BASH, '-e', '-o', 'pipefail', '-c', script], cwd=root,
                                    env=environment, capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            release = root / 'release'
            images = dict(line.split('=', 1) for line in (release / 'release.env').read_text().splitlines())
            calls = (root / 'docker.log').read_text()
            for engine in ('conpot', 'dionaea'):
                image = f'honeyforge/{engine}-decoy:testsha-123-1'
                self.assertEqual(images.get(f'DECOY_{engine.upper()}_IMAGE'), image)
                self.assertIn(f'--target {engine} -t {image}', calls)
                self.assertIn(image, next(line for line in calls.splitlines() if line.startswith('save ')))
            self.assertTrue((release / 'compose.native.yml').is_file())
            self.assertTrue((release / 'images.tar.gz').is_file())

    @unittest.skipUnless(shutil.which('docker'), 'Requires Docker Compose CLI')
    def test_native_images_reach_existing_orchestrator_and_are_required(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            manifest = root / 'compose.yml'
            manifest.write_text('''services:
  api:
    image: example/api:test
  orchestrator:
    image: example/orchestrator:test
    environment:
      DECOY_TCP_IMAGE: example/tcp:existing
''', encoding='utf-8')
            images = {'DECOY_CONPOT_IMAGE': 'example/conpot:release', 'DECOY_DIONAEA_IMAGE': 'example/dionaea:release'}
            environment = {key: value for key, value in os.environ.items() if key not in images}
            release = root / 'release.env'
            command = ['docker', 'compose', '--project-name', 'native-deployment-test', '--env-file', str(release),
                       '-f', str(manifest), '-f', str(ROOT / 'deploy/compose.native.yml'), 'config', '--format', 'json']
            release.write_text(''.join(f'{key}={value}\n' for key, value in images.items()), encoding='utf-8')
            result = subprocess.run(command, env=environment, capture_output=True, text=True, check=True)
            services = json.loads(result.stdout)['services']
            for key, value in images.items():
                self.assertEqual(services['orchestrator']['environment'][key], value)
            self.assertEqual(services['orchestrator']['environment']['DECOY_TCP_IMAGE'], 'example/tcp:existing')
            self.assertNotIn('environment', services['api'])
            for missing in images:
                with self.subTest(missing=missing):
                    release.write_text(''.join(f'{key}={value}\n' for key, value in images.items() if key != missing), encoding='utf-8')
                    result = subprocess.run(command, env=environment, capture_output=True, text=True)
                    self.assertNotEqual(result.returncode, 0)
                    self.assertIn(missing, result.stderr)
