# OWON Control

Control an **OWON HDS2202S** over USB from your browser, CLI, or gRPC.

![OWON WebUI showing live measurements and channel controls](docs/screenshots/webui-desktop.png)

*Actual WebUI with a simulated instrument backend. Measurements are demo data.*

## Quick start (Linux)

Install Go 1.26+. On Debian/Ubuntu, install dependencies and build:

```sh
sudo apt install git build-essential pkg-config libusb-1.0-0-dev usbutils
git clone https://github.com/xaionaro-go/owon.git
cd owon
CGO_ENABLED=1 go build -o bin/ ./cmd/...
```

Plug in your instrument and [grant USB access](docs/setup.md#usb-access).
Run these in separate terminals from the checkout:

```sh
./bin/owond
```

```sh
./bin/owonweb
```

Open **<http://127.0.0.1:8080/>**. Stop both processes with Ctrl+C when finished.
One matching instrument is auto-detected. With several connected, add `--serial SERIAL`; the error lists their serials.
The daemon and clients share a private per-user Unix socket; no socket setup or address flags needed.
For CLI access while the daemon runs:

```sh
./bin/owonctl info
```

Waveform captures contain raw bytes, not a decoded plot. Other models and firmware may differ.

- [USB access, troubleshooting, and systemd setup](docs/setup.md)
- [CLI, API, remote connections, and device limitations](docs/reference.md)
- [Home Assistant integration](docs/home-assistant.md)
- [Development and tests](docs/development.md) · [gRPC schema](pkg/owonrpc/owon.proto) · [USB protocol evidence](docs/usb-protocol.md)

<details>
<summary>WebUI at phone width</summary>

<img src="docs/screenshots/webui-mobile.png" alt="OWON WebUI at 390 pixels wide, showing the live device status and measurements" width="390">

Actual WebUI with the same simulated instrument backend; demo measurements.

</details>

License: [CC0 1.0 Universal](LICENSE). Dependencies retain their own licenses.
