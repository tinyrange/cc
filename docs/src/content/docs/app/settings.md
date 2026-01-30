---
title: Settings
description: Configuring ccapp application settings
---

ccapp has two levels of configuration: user settings stored per-user, and site configuration for enterprise deployments.

## User Settings

### Accessing Settings

Open the settings dialog from the launcher by clicking the gear icon.

### Available Settings

| Setting | Description | Default |
|---------|-------------|---------|
| Auto-update | Check for and install updates automatically | Enabled |
| Onboarding | Show first-run setup wizard | (one-time) |
| Snapshot cache | Cache VM boot snapshots for faster startup | Disabled |

### Settings File Location

User settings are stored in:

- macOS: `~/Library/Application Support/ccapp/settings.json`
- Linux: `~/.config/ccapp/settings.json`
- Windows: `%APPDATA%\ccapp\settings.json`

### Settings File Format

```json
{
  "onboarding_completed": true,
  "auto_update_enabled": true,
  "snapshot_cache_enabled": false
}
```

## Bundle Settings

Each bundle has its own settings accessible via the gear icon on the bundle card.

### Bundle-Specific Options

From the bundle settings dialog, you can:

- View bundle details (name, location)
- Delete the bundle

### Editing Bundle Configuration

To change boot options (memory, CPUs, command), edit the `ccbundle.yaml` file directly in the bundle directory. See [Bundles](/app/bundles/) for the full configuration reference.

## Site Configuration

For enterprise deployments, a `site-config.yml` file can be placed next to the application to pre-configure settings.

### Location

Place `site-config.yml` in the same directory as the ccapp executable/bundle:

- macOS: `/Applications/CrumbleCracker.app/../site-config.yml`
- Linux: Same directory as the executable
- Windows: Same directory as the executable

### Site Configuration Options

```yaml
# Skip the onboarding wizard
skip_onboarding: true

# Override auto-update setting (true/false/null for user choice)
auto_update_enabled: false
```

### Use Cases

Site configuration is useful for:

- **Enterprise deployments**: Pre-configure settings across an organization
- **Kiosk mode**: Skip setup for automated environments
- **Managed updates**: Disable auto-update when managing updates centrally

### Security

The site config file:

- Must not be world-writable (refused on Unix)
- Is loaded from the application directory only
- Cannot override security-critical settings

## Onboarding

The first-run onboarding wizard:

1. Welcomes new users
2. Explains key features
3. Optionally installs the app to Applications (macOS)
4. Sets up URL handler registration

### Skipping Onboarding

To skip onboarding:

1. Set `skip_onboarding: true` in site config, or
2. Manually set `"onboarding_completed": true` in settings.json

## Auto-Update

When enabled, ccapp:

1. Checks for updates on startup
2. Shows a notification when an update is available
3. Downloads and installs updates in the background
4. Restarts to apply the update

### Disabling Auto-Update

Via settings dialog or by setting `auto_update_enabled: false` in site config.

When disabled, you must manually download updates from the releases page.

### Update Source

Updates are downloaded from GitHub Releases with checksum verification.

## Logging

ccapp writes logs to the cache directory:

- macOS: `~/Library/Caches/ccapp/ccapp-YYYYMMDD-HHMMSS.log`
- Linux: `~/.cache/ccapp/ccapp-YYYYMMDD-HHMMSS.log`
- Windows: `%LOCALAPPDATA%\ccapp\ccapp-YYYYMMDD-HHMMSS.log`

### Accessing Logs

Click "Open Logs" in the settings to open the logs directory in your file manager.

### Log Content

Logs include:

- Application startup information
- Bundle loading and VM boot progress
- Error messages and stack traces
- Network and hypervisor status

## URL Handler

ccapp registers itself as a handler for `crumblecracker://` URLs. This enables:

- Web-based VM launching
- Deep linking to specific VMs
- Integration with external tools

### URL Format

```
crumblecracker://run?image=alpine:latest
```

### Security

URL-triggered VM launches require user confirmation before execution.

## Next Steps

- [Bundles](/app/bundles/) - Configure individual VMs
- [Overview](/app/overview/) - Return to app overview
