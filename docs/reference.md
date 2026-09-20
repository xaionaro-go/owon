# Runtime and API reference

[Quick start](../README.md) · [USB and service setup](setup.md) · [Development](development.md)

## USB transactions and timeouts

The daemon auto-selects the sole matching VID/PID device. With multiple devices,
it lists their serials and requires `--serial SERIAL`; no match is an error.
An explicit serial selects only that device. The USB descriptor serial must match
the SCPI identity before requests are served, and the verified serial is pinned
for reconnects. Every command carries an explicit
response framing mode. An ambiguous USB transaction poisons its session; the
next request must reopen and validate the same serial and SCPI identity. The
ambiguous command is never replayed.

Each device exchange and recovery phase has a configurable finite budget:
`owond --device-timeout` defaults to `10s` and requires a positive Go duration
(for example, `30s`). The budget starts after transport admission and covers
the complete exchange, including short writes, fragmented reads, and quiet
probes; it is not renewed for each transfer. Initial USB identity validation
uses the same policy. An earlier caller deadline always wins. This is not a
whole-transaction, subscription, or SSE lifetime limit: a healthy long-lived
stream can span many device-operation budgets.

The library configures this through `owonusb.Config.OperationTimeout` or
`owonsession.New(backend, owonsession.Config{ExpectedSerial: backend.Serial(), OperationTimeout: timeout})`.
Library zero selects `owonsession.DefaultDeviceOperationTimeout` (`10s`); negative values
are rejected. Expiry during an ambiguous exchange poisons the session without
replaying the command. Subscription device timeouts produce retryable
`DEVICE_UNAVAILABLE` and terminal gRPC `DeadlineExceeded`, allowing a later
request to attempt identity-checked recovery. Ten seconds is application
policy, not a measured maximum device latency.

## Keeping the instrument awake

`owond` enables the startup policy by default for firmware that supports the
observed shutdown-time dialect. Pass `--no-keep-awake` to leave the instrument
shutdown timer untouched. Before serving RPCs, the daemon
queries `:SHUTdown:TIMe?`; every valid reply (`10min`, `30min`, `60min`, or
`Unlimited`) permits exactly one `:SHUTdown:TIMe UNLIMITED` write followed by a
fresh readback that must be
`Unlimited`. Empty, unknown, or unexpected replies fail startup closed, and a
failed write is never replayed.

The policy is enabled by default and does not run a periodic SCPI ticker. It
applies once per daemon startup; restarting the daemon rechecks the timer. It does not claim
that a firmware power cycle persists the setting or that a later in-process
transport recovery reapplies it. The policy is therefore an explicit
best-effort control for firmware accepting the exact `UNLIMITED` token, not a
guarantee about every model or firmware revision.

For a DMM function patch, session admission remains governed only by the
caller's cancellation and deadline. After admission, the configured
`OperationTimeout` supplies the convergence budget for CONFIGURE, the initial
two-observation barrier, remaining writes, pacing, and (when remaining settings
exist) the final two-observation barrier. A function-only patch completes after
its initial barrier; it does not perform a duplicate final barrier.

Cancellation is cooperative: native libusb cancellation can still wait for its
completion callback, and synchronous discovery/cleanup cannot be forcibly
interrupted by the Go context. The timeout requests cancellation but does not
guarantee a hard wall-clock stop. Ownership remains held until native work
returns; no detached operation is allowed to overlap a replacement session.

## Browser and HTTP API

Open <http://127.0.0.1:8080/>. HTTP defaults to loopback. To expose it on your
network, run `owonweb --listen 0.0.0.0:8080` and open the server's LAN address
in your browser. Explicit LAN addresses and IPv6 listeners are also supported.
HTTP has no authentication or encryption: everyone who can reach the listener
can control the instrument. Choose the bind address and network access accordingly.
For authenticated HTTPS, configure a separate reverse proxy and keep the bridge
bound to an address accessible only by that proxy.
The `--ca`, `--cert`, `--key`, and `--server-name` flags configure only the native
gRPC connection to a remote TLS-enabled daemon. API routes are `/api/device`,
`/api/state`, `/api/execute`, `/api/channel`, `/api/acquisition`,
`/api/horizontal`, `/api/trigger`, `/api/measurement`, `/api/generator`,
`/api/dmm`, `/api/dmm/measurement`, `/api/run`, `/api/stop`, `/api/single`, `/api/auto`,
`/api/waveform`, and `/api/events` (SSE).

The bridge is intentionally not a direct browser gRPC connection: browsers do
not speak the daemon's native gRPC transport. API requests validate same-origin
and browser Fetch Metadata when provided. SSE uses `state`, `waveform`, `waveform-gap`,
`service-error`, and `stream-error` event names; `service-error` is a daemon
reported operation failure and `stream-error` covers transport setup/receive
failures plus invalid daemon events. Subscribe options are bounded (20 ms–1 hour,
queue capacity 1–1024, two waveform channels, and fourteen measurement
selectors).

The console keeps connection status, acquisition actions, live metrics, channel
controls, and acquisition/trigger controls visible. Measurements, stream
options, generator/DMM configuration, captures, raw commands, and diagnostics
use native one-level disclosures and remain available without changing the API
contract. Structured `service-error` and `stream-error` events retry only when
their `retryable` value is the literal JSON boolean `true`; omitted, null,
false, non-boolean, unknown, or malformed values stop the subscription. `Unavailable`,
`Aborted`, `DeadlineExceeded`, and `DataLoss` are retryable transport failures;
`ResourceExhausted` and invalid event payloads are terminal by design. Native
`EventSource` `CONNECTING` transitions retain the browser's reconnect path. On
`OPEN`, the console waits for a fresh state event before reporting live; after a
terminal failure it keeps rendered state visible but labels it stale.

### Native-control parity boundary

The WebUI distinguishes controls that are device-backed from controls that are
host-local presentation state. The HDS200 manual documents front-panel Auto,
cursor, save/BMP, calibration, and DMM Hold actions. The HDS200 SCPI reference
documents acquisition, trigger, measurement, generator, and DMM range/relative
commands, but does not document remote Auto, cursor, save/BMP, calibration, or
Hold commands. Average mode is serialized from the cached
`:ACQUIRE:MODE AVERage` reference: the controller writes
`:ACQUIRE:MODE AVERAGE` and then queries `:ACQUIRE:MODE?` in the same
transaction. A mismatched observed mode is unsupported, reaches the bridge as
gRPC `Unimplemented`/HTTP 501, and must not be rendered as Average applied; the
WebUI keeps the observed state and the error reason. Average count remains
unavailable. The bridge does not send guessed versions of unsupported commands:
a remote action is either backed by a typed command/readback contract, or is a
typed no-response candidate whose acceptance and resulting state remain
explicitly unverified, or is shown as unavailable with the source/device reason.

DMM Hold in the WebUI is explicitly a host hold: it freezes the last fresh value
and its capture timestamp in the browser and does not change the instrument's
measurement engine. The button and status use the words “Host hold” so this
presentation state is not confused with native device Hold. The automatic/manual
range selector is device-backed and uses the observed `:DMM:AUTO` and
`:DMM:RANGE` dialect. Typed state accepts only ON/mV/V; an unrecognized range
keeps `DMM_RANGE_UNSPECIFIED` plus its raw observed token (for example, raw
`2V` remains unknown rather than being normalized to `DMM_RANGE_V`), and the UI
renders `Unknown (raw: …)` or `Range unavailable` rather than inventing an enum.
Screen cursors remain
vendor screen-coordinate cursors unless a future device/source gate proves
voltage/time calibration; they never claim native `ΔV`/`ΔT` units from pixels.
The capture card also offers browser-local CSV/SVG exports of the verified screen
trace (x slots, y counts, and ground position), alongside the opaque bytes and
header. These exports are not calibrated ADC/voltage/time files; native CSV/BMP,
reference-memory, and settings files remain unavailable through the documented
SCPI bridge.

Live screen traces open alongside initial hydration and request CH1 and CH2 at
the existing one-second requested interval. Capture rate is not guaranteed.
Matching supplied geometries share one grid; different geometries use separate
panels. These are vendor screen coordinates, not calibrated samples. Channel
captures are sequential, not synchronized or XY; each label shows its host I/O
start/end and the browser's monotonic time since receipt. CH1 uses a solid line
and triangle ground marker; CH2 uses a dashed line and diamond.

Each channel retains only its latest capture. Gaps, reconnects, terminal errors
and page suspension mark retained traces stale. A channel also becomes stale
after the greater of three seconds or three requested intervals without receipt;
state-only traffic cannot freshen it. Unsupported or malformed traces replace
that channel's plot with the supplied reason or a rendering error, preserving
independently valid raw bytes and headers. Plotting is limited to 16,384 points
and 128 divisions per axis; raw data and headers are each limited to 8 MiB.

Manual capture retains one separate snapshot and selects it for raw inspection;
it adds no live plot. The raw-source selection stays fixed during stream updates.
Header and byte inspections are frozen, labeled snapshots; downloads resolve
the selected capture at click time. Browser rendering adds no waveform polling
or vendor-byte decoding. Saved CH1 replay proves the display plumbing; physical
comparison, other modes, native BMP capture and calibration remain unverified.

Control forms submit edited fields, not a copy of observed device state. A
generator timing or shape edit also sends the selected waveform; voltage/current
DMM settings send their selected function and AC/DC pair. Missing or conflicting selections
are reported before a request is sent. Successfully acknowledged fields are
omitted from later patches unless edited again (apart from required companions);
edits made while a request is pending remain pending. DMM range labels `ON`,
`mV`, and `V` are command tokens, not inferred measurements. The console enables
automatic range on DMM writes; disabling it is unsupported.

The generator offers the eight vendor-labelled Built-in choices alongside the
four standard waveforms. Frequency and period are mutually exclusive pending
edits; local bounds match the retained backend application policy. Built-in
shape modifiers remain unsupported. Persistent field-local errors prevent
invalid requests; clearing a valid field withdraws its pending write. An
unfinished numeric entry remains invalid, not silently cleared. Pending and
previously sent markers describe requests, never observed settings. Only Apply
is disabled during delivery; newer edits survive acknowledgements, including
changes away and back. Empty Apply sends nothing. Dispatched failures report
possible partial or unknown delivery and are never automatically retried.

Each control patch is validated before its ordered device writes begin. A
transport or device failure during execution can leave earlier writes applied;
the operation is not atomic and does not roll hardware state back. Successful
legacy requests do not manufacture typed generator or DMM configuration. The
optional generator `observe` request mode, which the WebUI enables, returns an
operation-local logical `FUNCTION?`/`CHANNEL?` readback and output-compensation
status. That result does not populate typed `GeneratorState`, prove a physical
output, or validate electrical behavior. Generator numeric fields remain
readback-unverified; symmetry writes use whole percentages from 0 through 100;
load and memory-depth selectors use only the supported command choices.
If an observed generator operation returns a typed partial result, the RPC/WebUI
response preserves its completed writes, logical observations, and compensation
status with no `appliedAt`; `PARTIAL_OR_UNKNOWN` is not a successful-state claim.

When a DMM patch changes function, the daemon keeps one admitted device
transaction while it waits for two consecutive matching function-query replies
before sending remaining relative/range/automatic-range writes. Only patches
with those remaining settings then check two more matching replies; a
function-only patch completes after its initial barrier. AC/DC replies are
query-context observations rather than independent proof of the active
voltage/current function. A timeout or malformed reply reports possible partial
or unknown application; it does not pretend that requested settings became
observed state.

## CLI and DMM

From the checkout, run `go run ./cmd/owonctl --help` to list every command and its
purpose. Each command also accepts `--help`; for example,
`go run ./cmd/owonctl waveform --help` documents its channel option.
With no arguments, `owonctl` prints the same generated help and succeeds.
Connection flags may appear before or after the subcommand. All three binaries
accept `--log-level` (default `info`); `debug` shows operational events and `trace`
adds entry/exit diagnostics. Structured go-belt/Logrus logs go to stderr, leaving
CLI response data on stdout. The daemon logs startup before opening USB and
reports readiness only after binding its listener. Web startup reports its actual
HTTP address; its daemon connection is established lazily on API requests.

Each CLI subcommand is implemented in its own package under
`cmd/owonctl/commands`.

The DMM command reads the instrument's current measurement when no configuration
flags are supplied. To select a mode and read it in one CLI invocation, pass
the typed function name; capacitance, for example, is:

```sh
go run ./cmd/owonctl --address tcp://127.0.0.1:50051 dmm --function capacitance
```

Supported functions are `voltage`, `current`, `resistance`, `capacitance`,
`diode`, and `continuity`. Voltage and current additionally require
`--current-type ac` or `--current-type dc`. Use `dmm --help` for the optional
relative, range, and automatic-range controls. Configuration and measurement are
separate RPCs; the returned function metadata is only what the daemon reports,
and is normally absent because the verified measurement response does not
identify its function. The CLI never labels a reading with the requested mode.

## Local and remote connections

Unix sockets and loopback TCP may run without TLS. Unix-socket access is
controlled by filesystem permissions; plaintext loopback TCP trusts every local
process that can connect. All three executables default to
`$XDG_RUNTIME_DIR/owon/owond.sock`. Without an absolute runtime directory whose
socket path fits Linux's 107-byte limit, they use `/tmp/owon-<effective-uid>/owond.sock`.
`TMPDIR` is ignored so different process environments do not split the endpoint.
The daemon creates the socket's parent with mode `0700`; an existing parent must
have that mode and belong to the current effective user. Clients do not create directories.
Run all three as the same user with the same `XDG_RUNTIME_DIR`; `--listen`, `--address`,
and `--grpc` override the respective endpoint. The WebUI's HTTP
server also trusts local apps, even when its daemon connection uses a Unix
socket; it has no local-user authentication. See [systemd setup](setup.md#run-as-a-systemd-service)
for an optional daemon Unix socket with group access.

Unix socket parent directories are part of the trusted boundary: each
component must be a directory and must not be group- or other-writable unless it
has the sticky bit (as `/tmp` does). The systemd unit creates `/run/owond` with
mode `0750` via `RuntimeDirectoryMode`; custom `--listen unix://...` paths must
meet the same requirement. The parent and the derived `.lock` path are expected
to remain trusted and stable; cooperating `owond` instances serialize ownership
through that lock. The socket itself is created with mode `0660`. A
non-loopback TCP listener is rejected unless `owond` receives
`--tls-cert`, `--tls-key`, `--tls-client-ca`, and at least one explicit client
identity allowlist through `--tls-client-san` or `--tls-client-sha256`. Remote
clients must receive `--ca`, `--cert`, `--key`, and `--server-name`. TLS flags are
rejected for Unix and loopback endpoints so their local trust boundary remains
unambiguous.

For example, a remote listener allowing one client DNS SAN and its matching
client invocation, run from the checkout, use absolute credential paths:

```sh
go run ./cmd/owond --serial YOUR_USB_SERIAL --listen tcp://0.0.0.0:50051 --tls-cert /etc/owond/server.pem --tls-key /etc/owond/server.key --tls-client-ca /etc/owond/client-ca.pem --tls-client-san collector.example
go run ./cmd/owonctl --address tcp://scope.example:50051 --ca /etc/owond/server-ca.pem --cert /etc/owond/client.pem --key /etc/owond/client.key --server-name scope.example info
```

## Device support and limitations

Waveform sample encoding is not asserted: the API carries raw bytes, captured
header JSON, and only independently verified metadata. This avoids corrupting
measurements by guessing sample width or scale.

Typed generator writes retain vendor-documented and community-transcribed grammar;
controlled tests prove serialization, not physical SET acceptance. Duty/rise/fall
interpretation remains uncertain. Individual generator queries exist, and the
optional `observe` mode returns only operation-local logical waveform/output
convergence; no complete validated mapping populates typed generator state. Numeric
generator fields remain readback-unverified. See the
[generator provenance table](usb-protocol.md#generator-command-provenance).
Screen-waveform reads remain narrower than the protobuf surface and do not guess
waveform-enum queries. Generator frequency, period, symmetry, duty, pulse-width,
rising-time and falling-time patches require an explicit waveform. Frequency and
period cannot be supplied together. The eight builtin waveform choices use
0.1 Hz–5 MHz or 200 ns–10 s and reject the five shape-modifier fields; see the
[typed generator rules](usb-protocol.md#builtin-selections-and-typed-request-rules).
For a waveform-only request, legacy write-only behavior sends no output or
frequency command. WebUI requests opt into operation-local logical readback,
which may query `FUNCTION?`/`CHANNEL?` and restore omitted output as its final
mutation. Observe requests containing only output or scalar fields remain
transport-complete/readback-unverified without a logical query in this bounded slice;
firmware transition behavior and electrical output remain unverified.
Clients needing
model-specific or newly documented SCPI can use the explicitly framed `Execute` escape
hatch. Subscription polls share a fair, cancellable server-wide weighted budget of 64
estimated transport commands per second by default; identity, requested measurements,
headers, and waveform header/data reads each contribute to the poll's weight.

The schema retains presence-aware fields for controls that differ across OWON
firmware. Average mode now has a source-backed write-and-verify path, but
average count and individual on-screen measurement-item selection remain
unavailable; a firmware mode mismatch fails with gRPC `Unimplemented` rather
than emitting guessed SCPI or claiming that Average was applied.

See [docs/usb-protocol.md](usb-protocol.md) for dated hardware evidence
and [docs/home-assistant.md](home-assistant.md) for Home Assistant usage.
