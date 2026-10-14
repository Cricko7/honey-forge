# Custom HTTP constructor

`custom-http/1` is a managed trap type. Operators build behavior using blocks on the
constructor board; deploying creates a regular profile and trap. Start, Stop,
configuration revisions, agent credentials, organization access, deployment node
placement and durable telemetry use the existing API and agent lifecycle.

The HTTP runtime and interpreter are Python. The agent adapter only launches the
runtime, relays acknowledged events and passive traffic copies, and stops it.

## Building a flow

Add an **HTTP input**, configure its unique service name and port, method and path
(`ANY` / `*` accept any method / path). Connect its `out` to behavior blocks and
finish every execution path with **HTTP response** or **Redirect**.

Available blocks:

| Block | Behavior |
| --- | --- |
| HTTP input | External HTTP entry, or internal handoff from another block |
| Parse | JSON, URL-encoded form, text, or content-type detection |
| Extract | Copy a selected field to a variable, optionally require it |
| Condition | Equal, unequal, contains, exists, numeric comparison, safe glob |
| Switch | Three configured values and a default branch; chain switches for more cases |
| Transform | Assign a JSON value/object, uppercase/lowercase, add, merge objects, remove a variable |
| State | Get, set or delete a session key |
| Call | Pass JSON to another HTTP input on the board; wait for and store its response |
| Send to separate trap | Forward JSON to another deployed custom-http trap's service and endpoint over authenticated agent channels |
| Emit event | Emit http.custom with a configured name, severity and typed JSON at this stage |
| Fork | Execute up to three branches concurrently with independent copies of request data |
| Join | Collect branch status/body results under a variable; chain forks for more branches |
| Delay | Wait 0–5000 ms, within the overall flow timeout |
| Response | Status, headers, Set-Cookie and JSON, text or HTML body |
| Redirect | 301, 302, 303, 307 or 308 with a templated Location |
| Error | Copy the caught error into a variable and continue to a response |

Nonterminal blocks expose optional `error` outputs. A missing required field,
invalid JSON, incompatible transformation or failed call follows that output when
connected. Otherwise the request receives 422 with a safe generic message. Overall
flow timeout produces 504. Malformed HTTP produces 400; oversized bodies produce
413; method/path mismatches produce 405/404. HEAD, 204 and 304 do not send a body.

## Data and templates

Selectors include `method`, `path`, `body`, `json.user`, `json.items.0.name`,
`vars.name`, `query.id`, `headers.authorization` and `cookies.name`. Header names
are lowercase. JSON is decoded by the Parse block. A Call supplies its decoded
payload directly as `json`, and supplies its serialization as `body`.

In a JSON template, an entire string `${selector}` is replaced with the selected
value **preserving its JSON type**. For example:

```json
{"user":"${json.user}","items":"${json.items}","count":"${json.count}"}
```

Embedding a selector inside a larger string converts it to text. Unknown selectors
produce `null`. JSON response strings are escaped by the JSON serializer, so request
data cannot inject extra JSON fields. Text and HTML templates are intentionally
literal; the operator owns their content.

Transform/State values and Call payloads are JSON templates, so copying an entire
object uses `"${json}"`. Call stores its response body in `vars.<key>` and status in
`vars.<key>_status`. JSON bodies are decoded back to their original type. Child
variables are isolated; the parent receives only the configured result. A direct
edge into another input continues using the current request data.

Fork branches must end in responses. Fork's `out` continues after all branches
finish. Join stores a result object keyed by `branch1`, `branch2`, `branch3`, each
containing `status` and `body`. Branch variables are isolated; session state is shared
within the session. Concurrent session writes use last-completed-write behavior.

State is in memory, keyed by the server-issued HttpOnly, SameSite=Lax `hf_session`
cookie, with 30-minute sliding retention, at most 1024 sessions and 16 KiB per session.
Runtime restart/Stop discards it. Sessions on different listener ports share the cookie
and state when the client uses the same hostname; separate managed traps do not.

## Boundaries and links

Each board compiles HTTP behavior into **one managed trap with multiple HTTP ports**.
Calls and direct handoffs connect those inputs internally. The **Send to separate
trap** block connects independently deployed traps, including traps on different
boards or deployment nodes. Configure its target trap, target HTTP input's service
name, method (POST/PUT/PATCH), endpoint and JSON template. Deploy validates the target
through the current operator's organization-scoped API and creates a directed
`/api/trap-links` permission. The receiver's installed input must accept that method
and path. The JSON is available directly as `json` and serialized as `body`.

For example: A's Input → Parse → Condition (`json.action == login`) → Send(B,
`web`, POST `/ingest`, `{"user":"${json.user}"}`) → Response(202). B's Input(`web`,
POST `/ingest`) → Transform → Condition → Send(C) → Response. Place **Emit event**
on any selected branch or after a transformation; only executions reaching it emit
`http.custom`. Deploy receivers before senders. The inspector lists the selected
receiver's configured services, methods, paths and ports.

Transport uses existing authenticated agent channels and local management pipes;
the decoy opens no outgoing sockets. Server checks each side's live credential,
connection, organization, installed configuration, directed link and endpoint.
Deleting a link revokes future sends. Passive copies on the same trap-link remain
separate from pipeline execution.

Send's `out` means acceptance into the server's bounded delivery queue; it does not
wait for the receiver's response. Missing links, stopped/offline/old agents, wrong
endpoints and full admission queues follow `error`. Delivery is asynchronous and
in memory, with no automatic retries or restart replay. An accepted delivery may
be discarded if the target stops, reconnects, changes its input or fills its local
queue before execution. Technical events at the receiver show processing/failure.
This mechanism does not provide durable or exactly-once workflow execution.

Chains are not tied to the local nested-call limit: independent receivers can
forward onward without growing a call stack. Each chain shares a server-issued
correlation ID, at most 4096 handoff attempts and a 30-second lifetime. Continuation
tokens are internal, bound to the receiving organization/trap, credential generation
and connection, and expire together;
an external request cannot supply one. Total live tokens are capped at 4096 per
server and queues at 16 per agent/runtime. Forwarded JSON is capped at 8 KiB including
JSON escaping. These bounds stop cycles and branching from consuming unbounded
resources. All participating agents/workers must run a build supporting pipelines.

There are at most 32 inputs and 512 blocks per board. Default execution limits are
512 total steps across all branches/calls, 10 seconds, 32 nested calls and 64 KiB
request/response/intermediate data. Cycles terminate at the limits. The descriptor
permits configuring up to 4096 steps and 30 seconds through profile configuration.
No Python code, expressions, arbitrary HTTP destinations or outgoing sockets are
available to blocks. Patterns use glob (`*`, `?`) rather than regular expressions.
Requests must use Content-Length; chunked transfer is rejected. Connections close
after one response. Request headers are bounded to 16 KiB and concurrent connections
to 64. Header splitting and overriding framing headers are rejected.

## Events

**Emit event** emits `http.custom`: `node_id`, configured `name`, configured
`severity` (low/medium/high/critical) and typed `payload`. Its payload is limited to
4 KiB including JSON escaping; overflow follows the block's `error` branch without
emitting a partial event. The server validates its name/severity against the issued
profile snapshot. Existing technical events always remain enabled.

Receivers emit `http.pipeline_received` with the sending trap, request ID and input
block ID. All receiver telemetry additionally contains `pipeline_id` and
`pipeline_step`, allowing correlation across separate traps. Received requests use
the target service/port and the original source address. Authentication headers,
operator credentials and request cookies are never forwarded.

Every connection emits `service.connection_opened` / `service.connection_closed`.
`http.request` records method/path and up to 4096 bytes of the request body;
`http.block` records block ID, kind, step, started/completed/failed phase and selected
branch; `http.response` records status and up to 4096 bytes of the actual response
body; `http.failure` records rejected requests and uncaught flow errors. Body captures
are Base64 and include truncation metadata. A request session ID correlates all
internal calls and branches, and session sequence orders the events.

Events must be acknowledged by the existing agent before the response is sent.
Delivery to the main server uses the existing durable buffer, retries and ingest
validation. A disconnected main server may delay delivery while the agent buffers it;
an unavailable telemetry buffer stops the runtime. Metadata continues to use the
entry service and port, while block IDs identify internal handoffs.
