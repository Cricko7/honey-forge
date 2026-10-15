"""Exercise the pinned install command without compiling native extensions."""
from pathlib import Path
import re
import shlex
import subprocess
import sys
import tempfile
import unittest


PREPARE = Path(__file__).with_name("dionaea-build.py")
# Install rule from Dionaea 4e459f1b672a5b4c1e8335c0bff1b93738019215.
INSTALL_RULE = r'install(CODE "execute_process(COMMAND ${PYTHON_EXECUTABLE} ${SETUP_PY} install --install-lib=\$ENV{DESTDIR}\${CMAKE_INSTALL_PREFIX}/${DIONAEA_PYTHON_SITELIBDIR})")'


class DionaeaBuildTests(unittest.TestCase):
    def source(self, root):
        (root / "src").mkdir()
        (root / "src/dionaea.c").write_text("threads = (threads <= 1?2:threads);\n")
        (root / "config.h.cmake").write_text("\n".join(
            f'#define {name} "${{{name}}}"'
            for name in ("DIONAEA_MODDIR", "DIONAEA_CONFDIR", "DIONAEA_PYTHON_SITELIBDIR")))
        module = root / "modules/python"
        module.mkdir(parents=True)
        (module / "CMakeLists.txt").write_text(INSTALL_RULE)
        return module

    def test_installed_package_and_bridge_share_import_path(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            module = self.source(root)
            prepared = subprocess.run([sys.executable, str(PREPARE)], cwd=root,
                                      capture_output=True, text=True)
            self.assertEqual(prepared.returncode, 0, prepared.stderr)
            package = module / "dionaea"
            package.mkdir()
            (package / "__init__.py").write_text("")
            (package / "core.py").write_text("MARKER = 'upstream'\n")
            setup = module / "setup.py"
            setup.write_text("from distutils.core import setup\n"
                             "setup(name='dionaea', version='0.11.0', packages=['dionaea'])\n")
            prefix = module / "installed"
            rule = (module / "CMakeLists.txt").read_text()
            command = re.search(r"COMMAND \$\{PYTHON_EXECUTABLE\} \$\{SETUP_PY\} (install [^)]+)\)", rule)
            self.assertIsNotNone(command)
            arguments = command.group(1).replace(r"\$ENV{DESTDIR}", "")
            arguments = arguments.replace(r"\${CMAKE_INSTALL_PREFIX}", "installed")
            arguments = arguments.replace("${DIONAEA_PYTHON_SITELIBDIR}", "lib/dionaea/python")
            result = subprocess.run([sys.executable, str(setup), *shlex.split(arguments)],
                                    cwd=module, capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            library = prefix / "lib/dionaea/python"
            # Docker copies the bridge here; the embedded interpreter inserts
            # only this library directory into sys.path (it never reads .pth).
            bridge = library / "dionaea/local_observer.py"
            bridge.parent.mkdir(parents=True, exist_ok=True)
            bridge.write_text("from .core import MARKER\n")
            probe = subprocess.run([sys.executable, "-I", "-c",
                                    "import sys; sys.path.insert(0, sys.argv[1]); "
                                    "from dionaea.local_observer import MARKER; "
                                    "assert MARKER == 'upstream'", str(library)],
                                   capture_output=True, text=True)
            self.assertEqual(probe.returncode, 0, result.stdout + result.stderr + probe.stderr)

    def test_rejects_changed_upstream_install_rule(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            module = self.source(root)
            (module / "CMakeLists.txt").write_text("# changed upstream install rule\n")
            result = subprocess.run([sys.executable, str(PREPARE)], cwd=root,
                                    capture_output=True, text=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("pinned source changed: modules/python/CMakeLists.txt", result.stderr)


if __name__ == "__main__":
    unittest.main()
