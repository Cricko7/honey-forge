import unittest
import tempfile
from pathlib import Path
from .install_constructor import INCLUDE, update_config


class ConstructorRouteTests(unittest.TestCase):
    def test_discovers_only_active_includes_and_resolves_symlinks(self):
        from .install_constructor import configuration_files
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'enabled').mkdir()
            active = root / 'active.conf'
            active.write_text('server {\n listen 443 ssl;\n server_name panel.example;\n}\n')
            (root / 'unused.conf').write_text('unused')
            (root / 'enabled' / 'site').symlink_to(active)
            config = root / 'nginx.conf'
            config.write_text('include enabled/*;\ninclude enabled/*;\n')
            self.assertEqual(configuration_files(config), [config, active])

    def test_selects_https_management_host_and_is_idempotent(self):
        source = '''server {
    listen 80;
    server_name panel.example;
    return 301 https://$host$request_uri;
}
server {
    listen 127.0.0.1:443 ssl;
    server_name panel.example;
    location / { root /var/www/panel; }
}
server {
    listen 127.0.0.1:443 ssl;
    server_name resources.example;
    location / { return 404; }
}
'''
        updated = update_config(source, 'panel.example')
        self.assertEqual(updated.count(INCLUDE), 1)
        self.assertNotIn(INCLUDE, updated.split('server_name resources.example')[1])
        self.assertEqual(update_config(updated, 'panel.example'), updated)

    def test_unknown_host_rejected(self):
        with self.assertRaises(ValueError):
            update_config('server {\n listen 443 ssl;\n server_name other.example;\n}', 'panel.example')
