`hds2202s-screen.json` retains the complete 600-byte CH1 payload and HEAD JSON
from the project's physical HDS2202S capture on 2026-09-19 at
14:44:42.701857440 UTC (`owon-completion-e2e/native-waveform.json`). It is
first-party instrument data, not code copied from vendor software.

The golden coordinates follow the vendor PC software's signed-even-byte display
algorithm: the first twelve Y values are 179,179,178,178,176,176,173,173,177,177,
176,176; ground Y is 178. Odd bytes differ in 292 of 300 pairs. Their electrical
meaning remains unverified; the fixture preserves them all.

Replaying this fixture verifies software plumbing, not physical calibration or
live USB operation. See `docs/usb-protocol.md` for the admitted profile and limits.
