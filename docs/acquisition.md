# Acquisition mode

The cached HDS200 SCPI reference documents the mode write
`:ACQUIRE:MODE AVERage` and the corresponding `AVERage` query token. The typed
compiler emits the case-insensitive equivalent `:ACQUIRE:MODE AVERAGE` and the
WebUI exposes `Average` as a mode. For an Average request, the controller keeps
one transaction, writes `:ACQUIRE:MODE AVERAGE`, then queries
`:ACQUIRE:MODE?`; it succeeds only when the parsed reply is Average. A different
reply is `ErrUnsupportedControl`, propagated as gRPC `Unimplemented` and HTTP
501, rather than reported as a successful application.

On 2026-09-20, a live OWON HDS2202S (serial `25061855`, firmware `V2.6.0`)
kept returning `SAMPle` after the Average request. The HTTP operation returned
Unimplemented/501, so the WebUI must retain the observed mode and must not report
Average as applied. This physical result is firmware evidence for that device,
not a claim about every firmware version.

Average-count writes remain unavailable because no validated count command or
readback contract is established. Average is the documented write-and-verify
exception above; other control writes still report transport completion only.

Reference: [HDS200 Series SCPI Protocol](https://files.owon.com.cn/software/Application/HDS200_Series_SCPI_Protocol.pdf).
