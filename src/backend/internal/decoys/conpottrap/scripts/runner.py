"""Starts exactly one trusted Conpot engine. No CLI, LogWorker or exporters."""
import importlib
import logging
import pathlib
import os
import sys
import tomllib
import types
import tempfile


def build_server(config, host, registry):
    import conpot
    conpot.__file__ = conpot.__file__.replace("\\", "/")
    import conpot.core as core

    service = config["services"][0]
    protocol = service["protocol"]
    root = pathlib.Path(conpot.__file__).parent / "templates" / "default"
    # The upstream package initializer eagerly imports every optional engine.
    # Load only the selected installed protocol; no CLI/exporter is imported.
    package = types.ModuleType("conpot.protocols")
    package.__path__ = [str(pathlib.Path(conpot.__file__).parent / "protocols")]
    sys.modules["conpot.protocols"] = package
    with (root / "template.toml").open("rb") as source:
        core.get_databus().initialize(tomllib.load(source))
    with (root / (protocol + ".toml")).open("rb") as source:
        template = tomllib.load(source)[protocol]
    template.update(host=host, port=service["port"])
    if protocol == "enip":
        template.update(mode="tcp", timeout=5.0)
    core.get_session = registry.get
    module_names = {"modbus": "modbus.modbus_server", "s7comm": "s7comm.s7_server", "enip": "enip.enip_server",
                    "snmp": "snmp.snmp_server", "bacnet": "bacnet.bacnet_server"}
    module = importlib.import_module("conpot.protocols." + module_names[protocol])
    if registry.transport == "tcp":
        module.serve_tcp_sync_handler = bounded_tcp(registry, registry.capacity, registry.max_seconds, protocol)
    else:
        # Avoid route-probe sockets for destination discovery in UDP loggers.
        module.get_interface_ip = lambda remote: host
        if protocol == "bacnet":
            module.serve_udp_datagram = bounded_udp(registry)
    if protocol == "snmp":
        from conpot.protocols.snmp import command_responder, conpot_cmdrsp
        from pysmi.reader import FileReader
        # Only packaged/precompiled MIBs; never resolve MIBs over HTTP.
        command_responder.HttpReader = lambda url: FileReader(str(root / "snmp" / "mibs"))
        conpot_cmdrsp.get_interface_ip = lambda remote: host
    args = SimpleNamespace(mibcache=tempfile.gettempdir(), config="", temp_dir=tempfile.gettempdir())
    classes = {"modbus": "ModbusServer", "s7comm": "S7Server", "snmp": "SNMPServer", "bacnet": "BacnetServer", "enip": "EnipServer"}
    return getattr(module, classes[protocol])(template, str(root), args)


async def run(config, host, channel):
    protocol = config["services"][0]["protocol"]
    transport = "udp" if protocol in {"snmp", "bacnet"} else "tcp"
    registry = Registry(channel, config["session"]["max_connections"], config["session"]["max_seconds"], transport)
    server = build_server(config, host, registry)
    task = asyncio.create_task(server.start(host, config["services"][0]["port"]))
    writer = None
    try:
        for _ in range(600):
            if task.done():
                await task
                raise RuntimeError("engine exited before readiness")
            ready = getattr(server, "_ready", None)
            if ready is not None and ready.is_set():
                break
            await asyncio.sleep(0.01)
        else:
            raise RuntimeError("engine readiness timeout")
        channel.output.write('{"kind":"ready"}\n')
        channel.output.flush()
        threading.Thread(target=channel.read_control, daemon=True).start()
        writer = threading.Thread(target=channel.write_events, daemon=True)
        writer.start()
        while not channel.stopping.is_set() and not channel.failed.is_set():
            if task.done():
                await task
                raise RuntimeError("engine exited")
            registry.expire(time.monotonic())
            await asyncio.sleep(0.1)
    finally:
        server.stop()
        try:
            await asyncio.wait_for(task, timeout=2)
        finally:
            registry.close_all()
        if writer is not None and not channel.failed.is_set():
            channel.pending.put_nowait(None)
            await asyncio.to_thread(writer.join, 3)
    if channel.failed.is_set():
        raise RuntimeError("observation channel unavailable")


def main():
    source, output = sys.stdin, sys.stdout
    bootstrap = source.readline((1 << 20) + 1)
    if len(bootstrap) > 1 << 20:
        raise RuntimeError("configuration exceeds capacity")
    data = json.loads(bootstrap)
    # Reserve stdout for the private pipe. Libraries must not write logs,
    # banners, credentials or protocol payloads to container logs.
    sys.stdout = open(os.devnull, "w")
    logging.disable(logging.CRITICAL)
    channel = Channel(source, output)
    asyncio.run(run(data["config"], data["host"], channel))


if __name__ == "__main__":
    main()
