#!/usr/bin/env python3
"""Check the three HoneyForge jury scenarios using only Python's standard library.

Run after deploying and starting the boards, with their receivers selected:
    python3 honeyforge-jury-check.py 147.45.141.37
HTTP 202 confirms acceptance; verify downstream events separately in the UI.
"""

import argparse
from dataclasses import dataclass
from datetime import datetime, timezone
import http.client
import json
import math
import socket
import time
import uuid


@dataclass(frozen=True)
class Check:
    label: str
    expected: int
    port: int
    path: str
    payload: str


def checks(run):
    cases = []

    def add(label, expected, port, path, suffix, **payload):
        if suffix:
            payload["run"] = run + "-" + suffix
        cases.append(Check(label, expected, port, path,
                           json.dumps(payload, ensure_ascii=False)))

    gateway = (32321, "/gateway")
    for label, expected, action, score, suffix in [
        ("Gateway: backup", 202, "backup", 25, "backup"),
        ("Gateway: internal health call", 200, "status", 25, "status"),
        ("Gateway: admin branch", 403, "admin", 25, "admin"),
        ("Gateway: high risk", 423, "backup", 90, "risk"),
        ("Gateway: unknown action", 422, "unknown", 25, "unknown"),
    ]:
        add(label, expected, *gateway, suffix,
            action=action, user="Alice", score=score)
    add("Gateway: missing score", 422, *gateway, "missing-score",
        action="backup", user="Alice")

    documents = (32311, "/documents")
    add("Documents: list", 200, *documents, "list",
        action="list", user="Alice", score=25)
    for label, file, suffix in [
        ("Documents: honeyfile -> gateway", "vault-demo.txt", "honeyfile"),
        ("Documents: ordinary file -> gateway", "budget-demo.csv", "download"),
    ]:
        add(label, 202, *documents, suffix,
            action="download", file=file, user="Alice", score=25)
    for label, expected, role, suffix in [
        ("Documents: auditor export", 202, "auditor", "export"),
        ("Documents: export denied", 403, "viewer", "denied"),
    ]:
        add(label, expected, *documents, suffix,
            action="export", role=role, user="Alice", score=25)
    add("Documents: missing file", 422, *documents, "missing-file",
        action="download", user="Alice", score=25)

    portal = (32301, "/identity")
    add("Portal: complete low-risk pipeline", 202, *portal, "chain",
        action="login", user="Alice", score=25)
    add("Portal: high-risk pipeline (downstream gateway: 423)", 202,
        *portal, "chain-risk", action="login", user="Alice", score=90)
    add("Portal: reset", 202, *portal, "reset", action="reset", user="Alice")
    add("Portal: service account", 403, *portal, "service",
        action="service", user="svc-demo")
    add("Portal: unknown action", 422, *portal, "portal-unknown",
        action="unknown", user="Alice")
    add("Portal: missing user", 422, *portal, None, action="login", score=25)
    cases.append(Check("Portal: malformed JSON", 422, *portal, "{invalid"))

    add("Directory: direct entry", 200, 32302, "/directory", "directory",
        user="Alice")
    add("Watermark: direct entry", 200, 32312, "/watermark", "watermark",
        user="Alice", file="vault-demo.txt")
    add("Health: direct entry", 200, 32322, "/health-check", "health",
        user="Alice")
    return cases


def request(host, case, connect_timeout, timeout):
    """No proxy or redirects; HTTP error responses are ordinary test results."""
    connection = http.client.HTTPConnection(host, case.port, timeout=connect_timeout)
    deadline = time.monotonic() + timeout

    def remaining():
        seconds = deadline - time.monotonic()
        if seconds <= 0:
            raise TimeoutError("request exceeded its time limit")
        transport_socket.settimeout(seconds)

    try:
        connection.timeout = min(connect_timeout, timeout)
        connection.connect()
        transport_socket = connection.sock
        remaining()
        connection.request("POST", case.path, body=case.payload.encode("utf-8"),
                           headers={"Content-Type": "application/json",
                                    "Connection": "close"})
        remaining()
        response = connection.getresponse()
        body = bytearray()
        while not response.isclosed():
            remaining()
            chunk = response.read1(65536)
            if not chunk:
                break
            body.extend(chunk)
            if len(body) > 1024 * 1024:
                raise ValueError("response exceeds 1 MiB")
        return response.status, body.decode("utf-8", errors="replace")
    finally:
        connection.close()


def positive_seconds(value):
    try:
        seconds = float(value)
    except ValueError as error:
        raise argparse.ArgumentTypeError("expected seconds as a number") from error
    if not math.isfinite(seconds) or seconds <= 0:
        raise argparse.ArgumentTypeError("timeout must be finite and positive")
    return seconds


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("host", nargs="?", default="147.45.141.37",
                        help="server IP or hostname, without scheme or port")
    parser.add_argument("--connect-timeout", type=positive_seconds, default=5.0)
    parser.add_argument("--timeout", type=positive_seconds, default=20.0,
                        help="time limit per request in seconds (default: 20)")
    args = parser.parse_args(argv)
    if not args.host or any(c.isspace() for c in args.host) or any(
            c in args.host for c in "/\\@?#"):
        parser.error("host must be an IP or hostname without a URL scheme or path")
    run = "jury-" + datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ")
    run += "-" + uuid.uuid4().hex[:8]
    cases = checks(run)
    print("Server:", args.host)
    print("Run prefix:", run, flush=True)
    failures = 0
    for index, case in enumerate(cases, 1):
        print("\n=== {}/{}: {}; expected HTTP {} ===".format(
            index, len(cases), case.label, case.expected), flush=True)
        try:
            status, body = request(args.host, case, args.connect_timeout, args.timeout)
            print(body)
            print("HTTP", status)
            if status == case.expected:
                print("PASS")
            else:
                failures += 1
                print("FAIL: unexpected HTTP status")
        except (OSError, http.client.HTTPException, ValueError) as error:
            failures += 1
            print("FAIL: {}: {}".format(type(error).__name__, error))
    print("\nPassed: {}/{}; failed: {}".format(len(cases) - failures, len(cases), failures))
    print("Verify downstream events separately in the platform; run prefix:", run)
    print("HTTP 202 means accepted, not that the entire pipeline has completed.")
    return 1 if failures else 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except KeyboardInterrupt:
        print("\nInterrupted")
        raise SystemExit(130)
