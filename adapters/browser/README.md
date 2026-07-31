# PixLog Browser Adapter

Load this directory as an unpacked Chromium extension, then configure the local
daemon URL and the token printed by `pixlog capture token`.

Capture is always user-triggered. The extension observes visible text controls,
selects, images, and an explicitly selected recent download. It does not inspect
background network traffic and records fidelity as `ui-observed`, never
`exact-request`. Use an official API or `pixlog capture proxy` when available.