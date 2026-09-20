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
The observed header reported `DATALEN=600`. Production preserves all raw bytes
and separately decodes the bounded vendor screen-coordinate profile below;
the ADC sample encoding and physical calibration remain unproved.

### Vendor-equivalent screen coordinates

The native decoder admits only `MODEL=HDS2202S_LS`, `DATATYPE=SCREEN`,
`SAMPLE.FULLSCREEN=600`, `DATALEN=600`, `TYPE=SAMPle`, and `SLOWMOVE=-1`,
with `SCREENOFFSET` absent or zero and exactly one requested channel carrying
an integer `OFFSET`. This is profile `owon-hds2202s-screen`: width 600, height
200, 12 horizontal divisions and 8 vertical divisions. Coordinates start at
the top left, X is the array index, `Y[i]=100-int8(data[i & ~1])`, and ground
Y is `100-OFFSET`. Coordinates outside the viewport are retained for clipping.
OFFSET must be in `[-2147483547,2147483748]` so the ground coordinate fits
signed 32-bit RPC coordinates. Integer tokens are parsed exactly, before
subtraction; fractional and overflowing integer OFFSET tokens are malformed.

This mapping follows `RapidDataImport.readData`, `AlphaWaveFormCurve.initPoints`
and the HDS2202S model geometry in OWON's [official HDS200 PC software](https://files.owon.com.cn/software/pc/HDS200_series_pc_software.zip),
plugin `com.owon.uppersoft.hds_1.2.11.1.v20240927.jar`. The first-party recorded
fixture in `pkg/owonscpi/testdata/hds2202s-screen.json` has 600 bytes; the
first twelve display Y values are 179,179,178,178,176,176,173,173,177,177,176,176
and ground Y is 178. Odd bytes differ in 292 of 300 pairs. They are preserved,
and their electrical meaning is not inferred from the vendor display algorithm.

Unproved models, geometry and acquisition modes return raw data with an explicit
`screen_trace_unavailable_reason`. Malformed JSON, field types, duplicate selected
channels, invalid offsets, or an admitted profile's payload-length mismatch are
dialect errors. Unknown numeric DATALEN values do not inherit the 600-byte profile's
length rule. Missing required profile fields are unsupported, not malformed.

`capture_started_at` is host time immediately before HEAD, and `captured_at` is
host time immediately after the channel payload arrives. They describe host I/O,
not device acquisition timing; channels are captured sequentially. Display slots
do not establish sample count, sample rate, volts calibration or trigger-relative
time. Raw encoding and scientific metadata retain their existing semantics.
Recorded-capture replay verifies native-controller, RPC and HTTP/SSE plumbing;
live-device comparison, PEAK/AVERAGE/roll fidelity and native BMP screenshots
remain unverified.

The following DMM exchanges also succeeded on the connected device:

```text
:DMM:CONFIGURE:VOLTAGE DC
:DMM:AUTO ON
:DMM:MEAS?
```

On a later read-only session against the same USB serial (`25061855`), the
device identified as `OWON,HDS2202S,25061855,V2.6.0`; `*ODN?` returned
`OWON,HDS2202S_LS,25061855,V2.6.0`; `:ACQUIRE:MODE?` returned `SAMPle`;
`:ACQUIRE:DEPMEM?` returned `4K`; `:DMM:AUTO?` and `:DMM:RANGE?` both
returned `10A`; `:DMM:REL?` returned `OFF`; and the generator queries returned
numeric values for frequency, period, amplitude, offset, high, low, symmetry,
width, rising time, falling time, duty cycle, and load. The raw capture is
`/home/pheona/tmp/owon-parity-20260920/read-only-queries-c.txt`.

On 2026-09-20, a live Average operation against that OWON HDS2202S wrote
`:ACQUIRE:MODE AVERAGE` and then queried `:ACQUIRE:MODE?`; the device still
reported `SAMPle`. The controller therefore returned unsupported/gRPC
`Unimplemented`, and the HTTP bridge returned 501 Not Implemented. The WebUI
must not report Average as applied; this is a dated physical observation, not a
universal firmware claim. A separate live DMM observation returned the raw range
token `2V`. Because the typed range enum recognizes only `ON`, `mV`, and `V`,
state preserves `DMM_RANGE_UNSPECIFIED` plus raw `2V`, and the UI must render it
as unknown raw evidence rather than infer `DMM_RANGE_V`.

Read-only queries for `:HOLD?`, `:AUTOSET?`, `:CURSOR?`, and `:SAVE?` produced
no ASCII response before the bounded exchange timeout and quarantined the USB
session. This is evidence that these spellings are not usable remote commands
on this firmware; it is not permission to retry alternate guessed setters.
The daemon was stopped afterward. The device then disappeared from USB, so a
new physical acceptance run requires a fresh manual reconnection.

An independent open-source HDS controller ([`seritools/owowon`](https://github.com/seritools/owowon), commit
`d66a522`) sends a no-response `:AUT .` sequence for its Auto action and notes
older `:AUToset` versus newer `:AUToseton` firmware spellings. This is useful
exploration evidence, not vendor authority or proof for V2.6.0. Any typed
Auto operation must therefore use an explicit no-response command. The typed
path currently selects the V2.5.1-backed `:AUToseton` candidate, keeps the
session poisoned on ambiguous delivery, and requires a live device acceptance
run before it is described as native-equivalent; V2.6.0 acceptance and
readback remain unverified.

The generic `:DMM:CONFIGURE?` query returned a device error during one function
transition, while later captures returned recognized generic tokens for
resistance, capacitance, diode, and continuity. Typed code uses that generic
query only for those four functions and uses the function-specific
voltage/current forms for AC/DC observations. An exact `error` reply is treated
as transient during a bounded two-consecutive-observation barrier; it is not a
verified full DMM-state readback and does not establish scalar units or range
readiness. Function-setting patches hold admission across the initial barrier;
remaining settings are sent only after it, and only such patches receive a
second final barrier. The configured session operation timeout supplies that
post-admission DMM convergence budget, while caller cancellation and deadlines
control admission.

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

Before a newly opened candidate receives `*IDN?`, recovery performs the same
five-millisecond bounded read probe while discarding already queued bytes. The
discard budget is one configured maximum length-prefixed frame plus its four-byte
prefix, and overflow, EOF, no-progress, mixed causes, or caller cancellation
rejects the candidate. This isolates bytes observed during the bounded window;
it is not a protocol synchronization barrier, so a reply arriving after quiet
has not been proven impossible.

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
| Cached vendor PDF | Documents waveform tokens, amplitude in volts peak-to-peak (Vpp), period/width in seconds and high-level syntax `:FUNCtion:HIGHt`. Symmetry writes are integer percentages but query replies are floating point. Duty has a flat heading and nested `:FUNCTION:PULSE:DTYCYCLE` syntax/examples; symmetry and width also differ between syntax and examples. No rise/fall sections were found. This is documentation, not a physical SET test. |
| Community transcription | Lists flat duty, `RISING`, `FALING`, and HDS2202S frequency bounds (0.1 Hz minimum; sine 25 MHz, square/pulse 5 MHz, ramp 1 MHz maxima). These are transcribed claims, not authoritative hardware semantics. |
| Vendor PC software | HDS plugin `com.owon.uppersoft.hds_1.2.11.1.v20240927.jar`, `IPref.uBuiltin` and `AGComposite` supply the eight exact builtin tokens and their shared 0.1 Hz–5 MHz ARB-category range. This is source-backed software behavior, not a physical SET or electrical test. |
| Vendor quantity editors | The same plugin's `IPref.uAmp` offers mVpp/Vpp; `AGComposite$4` converts amplitude to Vpp and sends `:function:ampl`, while its high-level editor sends volts with `:function:high`. `AGComposite$8` sends flat `:function:symmetry`, `:function:WIDTh` and `:function:dtycycle`, with width converted to seconds and duty left as a percentage. `AGComposite$10` sends `:FUNCtion:RISing` and `:FUNCtion:FALing` after conversion to seconds. These establish vendor-software spelling and intended quantities, not firmware acceptance or measured output. |
| Retained application policy | Existing command grammar and frequency bounds are unchanged. Inclusive 0–100 duty/symmetry validation is an application rule, not a proven inclusive physical range. Symmetry writes remain integer-valued; its state field remains double-valued. |
| Controlled-backend tests | Prove exact serialized commands, optional-field handling, validation and no I/O on invalid patches. They do not prove the instrument accepts a SET or that duty/rise/fall values have the assumed physical interpretation. |
| Read-only native observations, 2026-09-19 | HDS2202S V2.6.0, serial 25061855: flat duty query returned raw `500.000`; nested duty query returned `DeadlineExceeded` with both client and device-operation budgets set to 10 seconds, so the result does not identify which timer fired. Subsequent rise and fall queries each returned raw `1.953120e-06`. These observations prove neither SET grammar nor units/ranges; `500.000` is not a validated percentage. No generator setting or output was changed. |

The PDF's mixed-case `HIGHt` notation identifies `HIGH` as the short form and
`HIGHT` as the long form; the vendor software's `high` spelling is not evidence
that the retained `HIGHT` command is a typo. Neither source proves firmware
conformance to both forms. The historical duty reply `500.000` remains unresolved;
the vendor's unscaled percentage writer does not justify dividing that reply by
ten or otherwise inferring a typed percentage.

Individual generator queries therefore exist, but no complete validated query-to-state
mapping populates typed generator state. The optional `observe` generator operation
uses only `FUNCTION?` and `CHANNEL?` for two-consecutive logical convergence and
operation-local output restoration; its result is not typed `GeneratorState`, SET
proof, or electrical validation. Query observations must not be synthesized from a
requested patch. Numeric generator fields remain readback-unverified because no
validated query/quantization mapping is established. Each `CHANNEL?` barrier is
bounded at eight observations; repeated unavailable or changing replies return
logical nonconvergence rather than waiting indefinitely. A partial observed result
preserves completed writes, observations, and compensation status without an
`appliedAt` timestamp; it does not claim successful device state.

### Builtin selections and typed request rules

The eight added native/protobuf waveform values append to the original four;
their exact SCPI selections are:

| Native suffix | Protobuf suffix | Value | `:FUNCTION` token |
| --- | --- | --- | --- |
| `AmpALT` | `AMP_ALT` | 5 | `AmpALT` |
| `AttALT` | `ATT_ALT` | 6 | `AttALT` |
| `StairDown` | `STAIR_DOWN` | 7 | `StairDn` |
| `StairUpDown` | `STAIR_UP_DOWN` | 8 | `StairUD` |
| `StairUp` | `STAIR_UP` | 9 | `StairUp` |
| `BesselJ` | `BESSEL_J` | 10 | `Besselj` |
| `BesselY` | `BESSEL_Y` | 11 | `Bessely` |
| `Sinc` | `SINC` | 12 | `Sinc` |

Every frequency, period, symmetry, duty, pulse-width, rising-time or falling-time
write requires an explicit waveform in the same patch. Frequency and period are
mutually exclusive even when reciprocal. These request relationships are intrinsic
validation errors (`ErrInvalidRequest`); they do not rely on device readback.

Frequency limits are inclusive 0.1 Hz through 25 MHz for sine, 5 MHz for square
and pulse, 1 MHz for ramp, and 5 MHz for each builtin. The same maximum determines
each period's inclusive minimum (`1/maximum` seconds); the maximum period is
10 seconds. Periods are compared directly, avoiding reciprocal overflow. Thus all
builtins admit 200 ns–10 s. Out-of-range values fail before any write.

The typed compiler rejects builtin symmetry, duty, pulse-width, rising-time and falling-time writes as
`ErrUnsupportedControl`; their meanings are unproved for these shapes. Existing
four-waveform modifier grammar remains unchanged. Amplitude, levels, offset, load
and output can still be patched without waveform. With legacy write-only behavior,
a waveform-only request sends no output or frequency command and only explicit output emits
`:CHANNEL`. WebUI requests set optional `observe`, so a waveform transition may add logical
`FUNCTION?`/`CHANNEL?` queries and a final operation-local restoration of omitted output.
An observe request containing only output or scalar fields remains transport-complete/readback-unverified
and does not issue a logical query in this bounded slice. Firmware transition behavior, physical output,
and numeric readback remain unverified.
The complete patch is compiled before ordered I/O, so an invalid final field
cannot cause earlier writes. A later transport failure can still mean partial or
unknown delivery; requests are not replayed automatically.

HTTP→RPC→controller fixtures prove token selection, request validation, write
omission, and the bounded logical operation ordering when `observe` is enabled.
They do not establish a complete typed generator-state mapping, SET acceptance,
native-panel confirmation, firmware transition behavior, or separately authorized
electrical waveform behavior.

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
