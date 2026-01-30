---
title: Terminal Mode
description: Working with the VM terminal interface
---

When you launch a VM in ccapp, the application switches to terminal mode, providing a full terminal interface for interacting with the running VM.

## Terminal Features

### Full Terminal Emulation

The terminal supports:

- **256 colors**: Applications can use the full color palette
- **Unicode text**: International characters and emoji display correctly
- **Scrollback buffer**: Scroll up to view previous output
- **Selection and copy**: Select text with the mouse

### Keyboard Input

Standard terminal keyboard shortcuts work:

- `Ctrl+C` - Send interrupt signal
- `Ctrl+D` - Send EOF
- `Ctrl+Z` - Suspend foreground process
- Arrow keys, Home, End, Page Up/Down

### Mouse Support

The terminal forwards mouse events to applications that support them (like tmux, vim with mouse mode, or ncurses applications).

## Notch Bar

The notch bar at the top of the terminal provides quick access to VM controls.

### Network Toggle

Click the network icon to enable/disable internet access:

- **Enabled** (default): VM can access external networks
- **Disabled**: VM can only communicate with host netstack

This is useful for running untrusted code in a network-isolated environment.

### Shutdown Button

Click the shutdown icon to stop the VM. A confirmation dialog appears to prevent accidental shutdowns.

## Window Sizing

The terminal automatically adjusts to the window size. When you resize the window:

1. The terminal view updates to the new dimensions
2. The guest is notified of the new terminal size
3. Applications that respect `SIGWINCH` will redraw

## Color Scheme

ccapp uses the Tokyo Night color scheme, designed for comfortable extended terminal use:

| Element | Color |
|---------|-------|
| Background | `#1a1b26` |
| Foreground | `#c0caf5` |
| Selection | `#33467c` |
| Cursor | `#c0caf5` |

The scheme provides good contrast while reducing eye strain.

## Session Lifecycle

### Starting

When you click a bundle, ccapp:

1. Loads the bundle configuration
2. Prepares the container filesystem
3. Starts the hypervisor and VM
4. Connects the terminal to the VM's console
5. Runs the configured command

The loading screen shows progress during this process.

### Running

While running:

- All terminal input goes to the VM
- Output from the VM appears in the terminal
- The VM has network access (unless disabled)
- The guest filesystem is fully writable

### Exiting

The session ends when:

- You click the shutdown button and confirm
- The VM's main process exits
- The VM crashes or times out

After exit, ccapp returns to the launcher screen.

## Common Workflows

### Interactive Shell

Most images default to starting a shell. You can run commands interactively:

```
/ # whoami
root
/ # apk add vim
/ # vim /etc/hosts
```

### Running Services

Start a long-running service:

```
# npm start
Server listening on port 3000...
```

The terminal shows service output. The VM continues running until you shut it down.

### Debugging

Enable dmesg output in the bundle configuration to see kernel messages, which can help debug boot issues:

```yaml
boot:
  dmesg: true
```

## Troubleshooting

### No Output

If the terminal is blank:
- Wait a few seconds for boot to complete
- Check if the command is producing output
- Enable dmesg to see kernel boot messages

### Garbled Text

If text appears garbled:
- Ensure the terminal type matches (`TERM=xterm-256color`)
- Try resizing the window to trigger a redraw
- Some applications may need `reset` to clear state

### Keyboard Not Working

If keyboard input doesn't register:
- Click inside the terminal window to focus it
- Check that you're not in a selection mode
- Some applications require specific key modes

## Next Steps

- [Bundles](/cc/app/bundles/) - Customize VM configuration
- [Settings](/cc/app/settings/) - Configure application preferences
