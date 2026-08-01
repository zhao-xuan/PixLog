# PixLog Demo Workspace

This generated repository contains a deterministic image history created with
real `pixlog run -- magick ...` commands.

```bash
pixlog diff HEAD~1 HEAD -- assets/hero.png
pixlog blame --point 760,145 assets/hero.png
pixlog recipe show --revision HEAD assets/ai-poster.png
bash scripts/apply-unsafe-edit.sh && pixlog check
```

The final command is expected to fail. Use `bash scripts/reset-demo.sh` to return
to the committed policy-compliant state.