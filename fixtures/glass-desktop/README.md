# Glass desktop fixture

This fixture boots Debian systemd, Xorg, and XFCE against cc's virtio-gpu and
virtio-input devices. The image includes a deterministic X11 input probe for
the command-line test, but the normal desktop does not start or display it.

Build and import it with:

```sh
CC_BIN=../build/vmsh/cc ./fixtures/glass-desktop/build.sh
```

Then start it through cc:

```sh
../build/vmsh/cc vm start --vnc --init systemd --timeout 3m glass-desktop glass-desktop
```

The command prints the loopback VNC address in the returned display state.
Use `glass capture`, `glass resize`, `glass clipboard-set`,
`glass clipboard-get`, `glass type`, and `glass click` to drive it without a
GUI VNC client. The desktop starts small clipboard and RandR bridges so normal
VNC viewer clipboard and window-resize operations reach the running X11
session.
