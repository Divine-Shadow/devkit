---
name: fleet-vr-mode
description: Enter or inspect VR mode on the Derp, Talos, Lich and Queen Arcade PCs, and recognize intentional WSL downtime before attempting compute recovery.
---

“Enter VR mode” means stop WSL on all four Arcade PCs within one 180-second
deadline. Customer service takes priority over their running compute work.

Read the installed command/config locator at
`/home/bayesartre/dev/distributed-product-workers/ci-auth-pool-local/fleet-mode/installation.json`
through the existing NixOS Windows interop or Linux execution interface. It
identifies the accepted Devkit command and static station configuration. Run its
`vr` invocation once from the controller outside those four target guests. Do not
spend the deadline discovering hosts, finding historical scripts or waiting for
builds to finish. Use `status` for observation without restoring WSL.

Report each station's observed WSL-off and restart-inhibited result. Preserve an
inaccessible or unverified result; neither a timeout nor a lost Linux connection
establishes shutdown. Mode bookkeeping can finish after shutdown and must not
hold up the customer. The installed locator must describe qualified supply;
if it is absent or incomplete, report the concrete installation gap rather than
claim a prepared command exists.

Before repairing or restarting WSL on these four PCs, read
`/home/bayesartre/.local/state/fleet-mode/mode.json`. A recorded VR intent means
leave the selected stations down. Read status and the station's Windows mode
record if clarification is needed. An operator request to return to compute is
required before restoring the retained service/task settings; a down guest is
not itself such a request. Keep other fleet members working normally.
