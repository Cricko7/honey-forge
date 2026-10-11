# Directed trap traffic links

In the network builder, drag from a trap's right output to another trap's top
input. Admins create or delete links; viewers can inspect them. The graph loads
links from PostgreSQL, so navigation and browser reloads preserve real links.
Demo mode keeps links in the existing in-memory mock backend.

`GET /api/trap-links` returns `{ "items": [{ "source_id": "...", "target_id": "..." }] }`.
`POST /api/trap-links` takes one such pair and returns 201 plus Location.
`DELETE /api/trap-links/{source}/{target}` returns 204. Mutations use the same
session, Origin and CSRF checks as traps. Duplicate directions return 409;
self-links return 422; inaccessible or deleted endpoints return 404. Both
endpoints must belong to the current organization. Removing one direction does
not remove the reverse direction. Deleted traps disappear from routing and lists.

For A -> B, A keeps serving its original connection. Every inbound byte read at
the TCP listener boundary is copied before parsing, independent of telemetry
capture settings and payload truncation. All four built-in trap runtimes support
the tap. SSH copies contain the original encrypted wire packets, not decoded
commands. Server responses are not copied. The target is a passive agent inbox;
copies are not injected into the target protocol and are not forwarded again.
This allows fan-out and cycles without amplification or altered responses.

The agent opts in with `agent.hello.payload.traffic_mirroring=true`. A supporting
backend replies with the same flag in `agent.welcome`. Agents send `traffic.frame`
containing connection_id, sequence, kind (open/data/close), remote_address,
local_address and base64 data. Each data frame carries at most 8192 bytes. The
backend determines source_id from the authenticated agent and target_id from
saved links, then emits `traffic.received` with source_id, target_id and the
nested frame. Operators and agents cannot supply arbitrary forwarding addresses.

`traffic.ack` reports queued_targets and skipped_targets. This is a best-effort
live mirror: disconnected or older agents and full queues skip copies. It does
not promise durable storage, replay, receipt or processing. Sender/local/backend
queues hold 64 frames each; sequence gaps identify loss. Links apply per frame,
so a link created mid-connection can start receiving data without an open frame.
Already queued frames may arrive after removing a link. Raw bytes are never
published as events, included in the operator live stream, or logged.

The target retains its latest 64 copies in `traffic.Inbox`, with an eviction
counter. Future agent logic can inspect `Inbox.Snapshot()`; callers of
`agent.Run` can provide `Options.TrafficInbox` to access this buffer. No protocol
processing is required for receiving copies.
