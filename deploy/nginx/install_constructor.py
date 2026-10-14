"""Install the shared-origin route in the existing HTTPS management virtual host."""
import argparse
import glob
from pathlib import Path
import re
from urllib.parse import urlparse

INCLUDE = 'include /etc/honey-forge/nginx/constructor.location.conf;'


def configuration_files(config):
    files, visited = [], set()
    prefix = config.resolve().parent

    def visit(path):
        path = path.resolve()
        if path in visited:
            return
        visited.add(path)
        files.append(path)
        for match in re.finditer(r'(?m)^\s*include\s+([^;]+);', path.read_text()):
            pattern = Path(match.group(1).strip().strip('"\''))
            if not pattern.is_absolute():
                pattern = prefix / pattern
            for included in sorted(glob.glob(str(pattern))):
                visit(Path(included))

    visit(config)
    return files


def update_config(source, hostname):
    # Limit selection to a server_name directive followed by this server's locations.
    names = list(re.finditer(r'(?m)^(\s*)server_name\s+([^;]+);', source))
    candidates = []
    for name in names:
        if hostname not in name.group(2).split():
            continue
        start = source.rfind('server {', 0, name.start())
        if start < 0:
            continue
        header = source[start:name.start()]
        if re.search(r'\blisten\s+[^;]*\b443\b', header):
            candidates.append(name)
    if len(candidates) != 1:
        raise ValueError('Expected one HTTPS management server in this nginx file')
    name = candidates[0]
    end = source.find('\n}', name.end())
    if end < 0:
        raise ValueError('Cannot find management server boundary')
    if INCLUDE in source[name.end():end]:
        return source
    if re.search(r'\blocation\s+/constructor/', source[name.end():end]):
        raise ValueError('Existing constructor route needs review')
    indent = name.group(1).split('\n')[-1]
    return source[:name.end()] + '\n' + indent + INCLUDE + source[name.end():]


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--origin', required=True)
    parser.add_argument('--backup', type=Path, required=True)
    parser.add_argument('--restore', action='store_true')
    parser.add_argument('--route-source', type=Path)
    args = parser.parse_args()
    if args.restore:
        target = Path((args.backup / 'source-path').read_text()).resolve()
        if not target.is_relative_to('/etc/nginx'):
            raise ValueError('Backup target is outside nginx configuration')
        target.write_bytes((args.backup / 'original.conf').read_bytes())
        return
    hostname = urlparse(args.origin).hostname
    if not hostname:
        raise ValueError('Management origin has no hostname')
    if args.route_source:
        route = Path('/etc/honey-forge/nginx/constructor.location.conf')
        route.parent.mkdir(parents=True, exist_ok=True)
        route.write_bytes(args.route_source.read_bytes())
    files = configuration_files(Path('/etc/nginx/nginx.conf'))
    candidates = []
    for filename in files:
        path = Path(filename).resolve()
        source = path.read_text()
        try:
            updated = update_config(source, hostname)
        except ValueError:
            continue
        candidates.append((path, source, updated))
    if len(candidates) != 1:
        raise ValueError('Cannot uniquely find HTTPS management nginx configuration')
    path, source, updated = candidates[0]
    if not path.is_relative_to('/etc/nginx'):
        raise ValueError('Management configuration is outside the mounted nginx directory')
    args.backup.mkdir(parents=True, exist_ok=True)
    (args.backup / 'source-path').write_text(str(path))
    (args.backup / 'original.conf').write_text(source)
    if updated != source:
        path.write_text(updated)


if __name__ == '__main__':
    main()
