import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


@unittest.skipUnless(shutil.which('docker'), 'Requires Docker Compose CLI')
class HTTPDeploymentTests(unittest.TestCase):
    def test_release_image_reaches_orchestrator_on_existing_server(self):
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
            release = root / 'release.env'
            image = 'honeyforge/http-decoy:release-test'
            release.write_text('DECOY_HTTP_IMAGE=' + image + '\n', encoding='utf-8')
            environment = {key: value for key, value in os.environ.items() if key != 'DECOY_HTTP_IMAGE'}
            command = [
                'docker', 'compose', '--project-name', 'http-deployment-test',
                '--env-file', str(release), '-f', str(manifest),
                '-f', str(Path(__file__).with_name('compose.http.yml').resolve()),
                'config', '--format', 'json',
            ]
            result = subprocess.run(command, env=environment, capture_output=True, text=True, check=True)
            services = json.loads(result.stdout)['services']
            self.assertEqual(services['orchestrator']['environment']['DECOY_HTTP_IMAGE'], image)
            self.assertEqual(services['orchestrator']['environment']['DECOY_TCP_IMAGE'], 'example/tcp:existing')
            self.assertNotIn('environment', services['api'])

            release.write_text('', encoding='utf-8')
            result = subprocess.run(command, env=environment, capture_output=True, text=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn('DECOY_HTTP_IMAGE', result.stderr)
