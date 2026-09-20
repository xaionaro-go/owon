# Auto / autoset

The typed `Auto` action sends the source-backed V2.5.1 candidate `:AUToseton`
with no response requested. The seritools/owowon Auto path documents
`:AUToseton` for newer firmware; this repository retains it as an explicitly
unverified candidate for V2.6.0:
[source path](https://github.com/seritools/owowon/blob/d66a5227799fcdb8e9a18cf93afbd9fba084ae76/src/device.rs#L495-L499).

That source establishes the write candidate only. This repository does not
claim that every OWON firmware accepts it, and does not add guessed readback.
A successful controller/RPC/WebUI result means transport completion; the
device's resulting settings remain unknown. The WebUI therefore reports that
the command was sent, that device response/readback is unavailable, and that
the effect is unverified.
