"""Small, asserted changes to the pinned upstream source before compilation."""
from pathlib import Path


def replace(path, before, after):
    source = Path(path).read_text()
    if source.count(before) != 1:
        raise RuntimeError(f"pinned source changed: {path}")
    Path(path).write_text(source.replace(before, after))


# Host CPU count must not exhaust the worker's PID limit.
replace("src/dionaea.c", "threads = (threads <= 1?2:threads);", "threads = (threads <= 1 ? 2 : (threads > 4 ? 4 : threads));")
# Resolve code/config paths independently of the writable working directory.
for name in ("DIONAEA_MODDIR", "DIONAEA_CONFDIR", "DIONAEA_PYTHON_SITELIBDIR"):
    replace("config.h.cmake", f'#define {name} "${{{name}}}"', f'#define {name} "${{CMAKE_INSTALL_PREFIX}}/${{{name}}}"')
# The embedded interpreter inserts this directory into sys.path without reading
# .pth files. Install a flat package so core and the copied bridge share it.
replace("modules/python/CMakeLists.txt", "${SETUP_PY} install --install-lib=",
        "${SETUP_PY} install --single-version-externally-managed --record=installed-files.txt --install-lib=")
