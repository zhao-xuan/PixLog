# PixLog Photoshop UXP Adapter

1. Run `pixlog capture token` and export the value as `PIXLOG_CAPTURE_TOKEN`.
2. Run `pixlog capture serve` from the image repository.
3. Add this directory in Adobe UXP Developer Tool and load the plugin.
4. Open **Plugins > PixLog Capture**, enter the same token, and start capture.

The plugin subscribes only to its tested event allow-list. Unsupported event names
are skipped for the installed Photoshop version. Raw Action Descriptors are sent to
the local daemon, redacted there, and stored by OID; the Photoshop UI thread does
not hash images or create visual diffs.