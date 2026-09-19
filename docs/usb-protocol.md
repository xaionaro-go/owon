# USB and SCPI evidence

This is historical evidence captured on 2026-09-17, not a claim that the
instrument is currently connected.

USB descriptor manufacturer text was not captured reliably and is not used for
selection. Discovery uses VID/PID plus an exact unique descriptor serial; after
claiming the interface it requires SCPI manufacturer `OWON`, expected model,
and the same serial before admitting commands.

## Descriptor

Local descriptor inspection reported VID:PID `5345:1234`, full-speed USB,
interface 0, bulk IN address `0x81`, and bulk OUT address `0x01`. Both bulk
endpoints reported 64-byte maximum packets. The descriptor product string was
`PDS6062T`, while SCPI identified the device as HDS2202S. Production discovery
therefore validates both the USB serial and `*IDN?` instead of trusting the USB
product string.

## Observed exchanges

For USB serial `25061855`:

```text
*IDN?
OWON,HDS2202S,25061855,V2.6.0

:DATA:WAVE:SCREEN:HEAD?
589-byte JSON payload after a four-byte little-endian payload length

:DATA:WAVE:SCREEN:CH1?
600-byte payload after a four-byte little-endian payload length
```

Scalar replies were LF-terminated. The header and waveform replies used a
four-byte little-endian payload length followed by exactly that many bytes.
The observed header reported `DATALEN=600`; the sample encoding was not proven,
so production code deliberately exports the waveform as raw bytes.

The following DMM exchanges also succeeded on the connected device:

```text
:DMM:CONFIGURE:VOLTAGE DC
:DMM:AUTO ON
:DMM:MEAS?
```

The generic `:DMM:CONFIGURE?` query returned a device error. Typed code
therefore uses the function-specific voltage/current forms and does not infer
the current DMM state from that generic query.

The instrument subsequently disconnected; the kernel reported USB disconnect
for device number 72. Reopen, stale-response, and reconnect behavior were not
validated against live hardware in that capture. Those paths are guarded by
deterministic transport tests and still require a future hardware E2E run.

For documented no-response commands, production transport observes a bounded
quiet period after the write and again before the next command. Only expiration
of the probe's own deadline with a single-cause timeout/native-cancellation
result counts as quiet. Joined errors are conservatively rejected, including
joins containing only expected timeout causes. EOF, caller cancellation, bytes,
and unrelated read errors poison the session. Surplus bytes retain any
concurrent endpoint error and ended caller context. Native `TransferCancelled`
on ordinary reads or writes retains its original error tree and adds the
ended context, so cancellation classification does not erase device failures.
This is bounded isolation, not a
protocol barrier: hardware behavior for a reply arriving after both probes is
unverified. Such a reply must not be assumed impossible until the device offers
a documented synchronization primitive.

The Go cleanup owner accepts context cancellation and checks it between native
libusb phases. Individual synchronous `gousb` close/enumeration calls are not
interruptible by that context. `owonsession.Session.CloseContext` therefore bounds the
caller's wait; a timeout does not prove that native cleanup has completed.

## Device-operation timeout policy

`owonsession.DefaultDeviceOperationTimeout` is ten seconds. `owonsession.Config.OperationTimeout`
and `owonusb.Config.OperationTimeout` share that default when zero; negative library
values are rejected. The daemon's `-device-timeout` uses the same default but
requires a strictly positive duration. This configurable application policy
is not a vendor latency guarantee or new hardware measurement.

Each complete exchange receives one child deadline after transport admission
and command validation. That same deadline covers all partial writes, framing
fragments, idle checks, and no-response probes. Each identity-checked recovery
receives a separate budget, including the initial identity validation before
the transport is constructed. Earlier parent deadlines are retained. Ordinary
admission/scheduler waiting uses caller cancellation rather than consuming a
device-operation budget. A multi-command operation or healthy subscription can
therefore outlive one budget; there is no stream-wide timeout.

An in-flight timeout remains an ambiguous exchange: the session is poisoned,
remaining commands stop, and the command is never replayed. Later work must
recover and prove USB serial plus SCPI serial/model/manufacturer first. A
failed recovery leaves the session poisoned. Native cancellation is cooperative:
gousb cancels an outstanding transfer and waits for its completion callback,
while synchronous discovery and cleanup phases cannot be forcibly interrupted.
The deadline requests cancellation, not a guaranteed hard stop; ownership is
retained until the native operation returns.

## Generator command provenance

The sources do not establish one uniformly verified command inventory.
The [vendor protocol PDF](https://files.owon.com.cn/software/Application/HDS200_Series_SCPI_Protocol.pdf)
and [community transcription](https://linux4life798.github.io/owon-hds200-capture/HDS200_Series_SCPI_Protocol.html)
are distinct evidence: the latter describes itself as a transcription with corrections,
not vendor authority. The live PDF fetch timed out during the 2026-09-19 audit;
the vendor statements below were checked against a cached copy.

| Evidence | What it establishes and what it does not |
| --- | --- |
| Cached vendor PDF | Documents waveform tokens and the unusual `:FUNCTION:HIGHT` spelling. Symmetry writes are integer percentages but query replies are floating point. Duty has a flat heading and nested `:FUNCTION:PULSE:DTYCYCLE` syntax/examples; symmetry and width also differ between syntax and examples. No rise/fall sections were found. This is documentation, not a physical SET test. |
| Community transcription | Lists flat duty, `RISING`, `FALING`, and HDS2202S frequency bounds (0.1 Hz minimum; sine 25 MHz, square/pulse 5 MHz, ramp 1 MHz maxima). These are transcribed claims, not authoritative hardware semantics. |
| Retained application policy | Existing command grammar and frequency bounds are unchanged. Inclusive 0–100 duty/symmetry validation is an application rule, not a proven inclusive physical range. Symmetry writes remain integer-valued; its state field remains double-valued. |
| Controlled-backend tests | Prove exact serialized commands, optional-field handling, validation and no I/O on invalid patches. They do not prove the instrument accepts a SET or that duty/rise/fall values have the assumed physical interpretation. |
| Read-only native observations, 2026-09-19 | HDS2202S V2.6.0, serial 25061855: flat duty query returned raw `500.000`; nested duty query returned `DeadlineExceeded` with both client and device-operation budgets set to 10 seconds, so the result does not identify which timer fired. Subsequent rise and fall queries each returned raw `1.953120e-06`. These observations prove neither SET grammar nor units/ranges; `500.000` is not a validated percentage. No generator setting or output was changed. |

Individual generator queries therefore exist, but no complete validated query-to-state
mapping populates typed generator state. Query observations must not be promoted to
SET proof or used to synthesize observed state from a requested patch.

## Offset writes and raw header values

The manual's integer-parameter warning explicitly excludes decimal writes.
The channel `:CH<n>:OFFSET` table specifies integer divisions in [-200,200];
this specific table takes precedence over the introduction's inconsistent
generic ±2000 example. `:HORIZONTAL:OFFSET` also takes integer divisions,
but the manual does not specify a fixed numerical range. The typed API uses
optional signed int64 `offset_divisions` fields for both commands and emits
exact decimal integers. The int64 limits are representation limits, not
verified horizontal device limits. Omission leaves the value unchanged;
explicit zero is a write.

Read-only observations on 2026-09-19 returned `-3.12` from `:CH1:OFFSET?`,
`-1.96` from `:CH2:OFFSET?`, and `0.00` from `:HORIZONTAL:OFFSET?`.
The corresponding screen-header channel `OFFSET` values were `-78` and `-49`.
These fractional query replies differ from the documented integer query
format; they do not establish that fractional SET commands are supported.
No fractional offset write experiment was performed, so hardware rejection
or acceptance of fractional writes remains unverified.

Header `OFFSET` and `HOFFSET` units and their conversion to write divisions
are undocumented. The apparent ratio at those two channel observations is
not a proven conversion. State exposes them as optional `screen_header_offset`
values, preserving raw numbers (including fractions), explicit zero, and
absence. They are not division-valued readback and never populate writable
browser controls. Reading controls still uses the single screen-header query;
it does not add offset queries or synthesize state from requested values.
