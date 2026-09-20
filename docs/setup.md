# USB access and service setup

[Quick start](../README.md) · [Runtime and API reference](reference.md)

## USB access

On Debian/Ubuntu, inspect the connected instrument with
`sudo lsusb -v -d 5345:1234` and note its `iSerial` value.
The daemon checks both the USB serial and the SCPI model (HDS2202S by default).

Create `/etc/udev/rules.d/70-owon.rules` with a privileged editor, for example
`sudo editor /etc/udev/rules.d/70-owon.rules`, containing this
line after replacing `YOUR_USB_SERIAL` with your instrument's USB serial:

```udev
SUBSYSTEM=="usb", ATTR{idVendor}=="5345", ATTR{idProduct}=="1234", ATTR{serial}=="YOUR_USB_SERIAL", GROUP="plugdev", MODE="0660", TAG+="uaccess"
```

The `uaccess` tag grants access to the active local desktop user. Ensure the
`plugdev` group exists. For a headless or SSH session, also add your login to
that group, then log out and back in (replace `YOUR_LOGIN`):

```sh
sudo groupadd -f plugdev
sudo usermod -aG plugdev YOUR_LOGIN
```

Reload the rules, then unplug and reconnect the instrument:

```sh
sudo udevadm control --reload-rules
```

The repository's [udev rule](../init/udev/70-owon.rules) and
[systemd unit](../init/owond.service) contain the original development
instrument's serial. Customize both before using them with your instrument.

## Troubleshooting

| Symptom | Check |
| --- | --- |
| No instrument found | Check the USB cable, `lsusb`, and the exact USB serial passed to `--serial`. |
| USB permission denied | Confirm the rule matches your serial; reconnect USB and restart your login after group changes. |
| Model mismatch | HDS2202S is the default. `--model` selects the expected SCPI model, but does not establish support for another model's commands. |
| Socket permission or unsafe-directory error | Run daemon and clients as the same user. The default socket parent must be owned by that user with mode `0700`. Custom socket parents must not be group/other writable unless sticky. See [connection details](reference.md#local-and-remote-connections). |
| WebUI disconnected | Keep `owond` running and use the same address for the daemon and bridge. Check their terminal errors. |
| Port 8080 busy | Start `owonweb` with `--listen 127.0.0.1:8081`, then open that port. |
| Device operation times out | The default budget is 10 seconds per operation; `owond --device-timeout 30s` raises it. See [timeout behavior](reference.md#usb-transactions-and-timeouts). |
| Instrument powers off | Ensure `owond` is not started with `--no-keep-awake`; startup runs one verified shutdown-timer policy by default. See [keeping the instrument awake](reference.md#keeping-the-instrument-awake). |

## Run as a systemd service

This is optional; the quick start needs no service installation. Complete the USB
setup above. A systemd unit must execute an installed binary, so build the daemon
once before installing it. Skip `useradd` if `owond` already exists. From the
checkout, install the daemon and example unit:

```sh
sudo useradd --system --home-dir /nonexistent --shell /usr/sbin/nologin --user-group owond
sudo groupadd -f plugdev
go build -o /tmp/owond ./cmd/owond
sudo install -m 0755 /tmp/owond /usr/local/bin/owond
sudo install -m 0644 init/owond.service /etc/systemd/system/owond.service
sudo editor /etc/systemd/system/owond.service
```

In the installed unit, replace the `--serial` value in `ExecStart` with your
instrument's USB serial. The unit uses
`plugdev` for USB access and creates `/run/owond` with mode `0750`.
The startup shutdown-timer policy is enabled by default. Leave `ExecStart`
unchanged when the instrument firmware accepts the documented `UNLIMITED`
shutdown-timer token. Add `--no-keep-awake` when the firmware does not support
that policy; otherwise the daemon will fail startup if the readback is malformed
or the verified write does not take effect.

To let your login use the service's socket, add it to the `owond` group, then
log out and back in. Members can control the instrument.
Stop any manually running daemon before starting the service:

```sh
sudo usermod -aG owond YOUR_LOGIN
sudo systemctl daemon-reload
sudo systemctl enable --now owond.service
```

Point clients at the system service's socket instead of the default per-user socket:

```sh
go run ./cmd/owonctl --address unix:///run/owond/owond.sock info
go run ./cmd/owonweb --grpc unix:///run/owond/owond.sock
```
