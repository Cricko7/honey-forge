"""Bounded, memory-only observations; no upstream logging/exporter is started."""
import json
import queue
import threading
import time
import uuid


class Channel:
    def __init__(self, source, output):
        self.source, self.output = source, output
        self.pending = queue.Queue(maxsize=64)
        self.acks = queue.Queue(maxsize=1)
        self.failed = threading.Event()
        self.stopping = threading.Event()

    def emit(self, item):
        if self.failed.is_set():
            raise RuntimeError("telemetry unavailable")
        try:
            self.pending.put_nowait(item)
        except queue.Full:
            self.failed.set()
            raise RuntimeError("telemetry capacity exceeded") from None

    def read_control(self):
        while True:
            line = self.source.readline(16)
            if not line:
                self.stopping.set()
                return
            if line == "stop\n":
                self.stopping.set()
                continue
            if line != "ok\n":
                self.failed.set()
                return
            try:
                self.acks.put_nowait(True)
            except queue.Full:
                self.failed.set()
                return

    def write_events(self):
        try:
            while True:
                item = self.pending.get()
                try:
                    if item is None:
                        return
                    self.output.write(json.dumps(item, ensure_ascii=True) + "\n")
                    self.output.flush()
                    self.acks.get(timeout=5)
                finally:
                    self.pending.task_done()
        except (OSError, ValueError, queue.Empty):
            self.failed.set()
