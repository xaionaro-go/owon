# Home Assistant integration

The lowest-maintenance integration is Home Assistant's `command_line` sensor
plus `shell_command` controls. Install `owonctl` on the Home Assistant host and
make the daemon Unix socket visible to the Home Assistant process. The process
must belong to the socket's group.

Example `configuration.yaml`:

```yaml
command_line:
  - sensor:
      name: OWON CH1 frequency
      unique_id: owon_ch1_frequency
      command: >-
        /usr/local/bin/owonctl
        --address unix:///run/owond/owond.sock
        execute --mode ascii ':MEASUREMENT:CH1:FREQUENCY?'
      unit_of_measurement: Hz
      scan_interval: 5
      command_timeout: 4

shell_command:
  owon_run: >-
    /usr/local/bin/owonctl
    --address unix:///run/owond/owond.sock run
  owon_stop: >-
    /usr/local/bin/owonctl
    --address unix:///run/owond/owond.sock stop
  owon_single: >-
    /usr/local/bin/owonctl
    --address unix:///run/owond/owond.sock single
```

Restart Home Assistant after configuration validation. For Home Assistant OS,
run `owond` and `owonctl` in an add-on or another host reachable over TLS TCP;
the core container cannot normally access arbitrary host binaries or sockets.

An external supervised installation can use an absolute command such as:

```yaml
command_line:
  - sensor:
      name: OWON remote identity
      unique_id: owon_remote_identity
      command: >-
        /usr/local/bin/owonctl
        --address tcp://scope.example:50051
        --ca /etc/home-assistant/owon/server-ca.pem
        --cert /etc/home-assistant/owon/client.pem
        --key /etc/home-assistant/owon/client.key
        --server-name scope.example
        info
      scan_interval: 60
```

For many high-rate entities, write a custom Home Assistant integration around
the generated Go/Python-equivalent protobuf contract or bridge the `Subscribe`
stream to MQTT discovery. The command-line route intentionally favors a small,
auditable setup over high-rate waveform transport.
