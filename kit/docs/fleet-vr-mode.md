# Arcade VR mode

The operator's “enter VR mode” request means force WSL off on Derp, Talos, Lich
and Queen within one 180-second deadline. These Windows PCs serve walk-in VR
customers. Running compute work does not postpone the shutdown.

The operator-selected command is:

```
fleet-mode vr --config /absolute/path/to/stations.json
```

Build the controller from `.#fleet-mode`. `status` observes the same stations
without starting WSL or changing mode.
The four Windows SSH endpoints and existing identities/host pins are configured
at installation, so an invocation does not rediscover hosts or keepalive scripts.
The controller must run outside the four guests it shuts down.

The Windows package is `.#fleet-mode-windows`. Its fixed installed paths are
`C:\ProgramData\FleetMode\fleet-mode.exe` and `agent.json` in the same directory.
That directory must permit writes only by Administrators and SYSTEM. The agent
checks the configured Windows hostname before making changes. Agent configuration
uses schemaVersion 1, name, expectedHostname, statePath and keepAliveTasks. The
statePath is `C:\ProgramData\FleetMode\mode.json`.

The agent inhibits restart of the exact `WSLService`, retains its prior startup
mode, disables/ends configured keeper tasks, runs `wsl.exe --shutdown --force`,
and stops the WSL service. Its escalation terminates only compute systems whose
actual HCS owner is WSL. It observes both WSL compute-system absence and absence
of vmmemWSL before reporting WSL off. It does not stop vmcompute or unrelated VMs.
Restart inhibition requires the WSL service to be disabled and stopped.

The controller dispatches all four calls concurrently with a 170-second shared
budget and a three-second local SSH cleanup allowance. The agent has a 160-second
budget; keepalive handling gets at most 20 seconds. Mode recording runs separately
and cannot delay shutdown. Receipts distinguish observed shutdown, restart
inhibition, recording failures, timeouts and inaccessible stations. An unreachable
or unresponsive Windows machine remains unverified; elapsed time alone is not
proof that it shut down.

The persistent controller and station mode records tell agents to leave the
Arcade hosts down. Recovery work must read the operator's mode before treating a
down guest as an outage. Compute restoration requires a later explicit operator
request; this command never automatically restores WSL. Restore the retained
service/task settings through the normal Windows interface before resuming
compute, then update mode after successful receiving.

Qualification requires the package's unit/race tests, a Windows build, read-only
status receiving on each endpoint, and a timed busy shutdown. Unit tests and an
API acknowledgement do not establish the live 180-second business guarantee.
