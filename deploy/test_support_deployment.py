import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


@unittest.skipUnless(shutil.which('docker'), 'Requires Docker Compose CLI')
class SupportDeploymentTests(unittest.TestCase):
    def test_private_environment_reaches_only_the_api(self):
        settings = {
            'SUPPORT_SMTP_HOST': 'smtp.example.test',
            'SUPPORT_SMTP_PORT': '587',
            'SUPPORT_SMTP_SECURITY': 'starttls',
            'SUPPORT_SMTP_USERNAME': 'support@example.test',
            'SUPPORT_SMTP_PASSWORD': 'test-only-password',
            'SUPPORT_SMTP_FROM': 'support@example.test',
        }

        for enabled in (True, False):
            with self.subTest(enabled=enabled), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                manifest = root / 'compose.yml'
                manifest.write_text('''services:
  api:
    image: example/api:test
    environment:
      EXISTING_SETTING: preserved
  constructor:
    image: example/constructor:test
''', encoding='utf-8')

                config = root / 'production.env'
                config.write_text(
                    ''.join(f'{key}={value}\n' for key, value in settings.items()) if enabled else '',
                    encoding='utf-8',
                )

                environment = {
                    key: value for key, value in os.environ.items()
                    if not key.startswith('SUPPORT_SMTP_')
                }
                override = Path(__file__).with_name('compose.support.yml').resolve()
                result = subprocess.run(
                    [
                        'docker', 'compose', '--project-name', 'support-test',
                        '--env-file', str(config),
                        '-f', str(manifest), '-f', str(override),
                        'config', '--format', 'json',
                    ],
                    env=environment,
                    capture_output=True,
                    text=True,
                    check=True,
                )

                services = json.loads(result.stdout)['services']
                api_environment = services['api']['environment']
                self.assertEqual(api_environment['EXISTING_SETTING'], 'preserved')

                for key, value in settings.items():
                    self.assertEqual(api_environment[key], value if enabled else '')

                self.assertNotIn('environment', services['constructor'])
