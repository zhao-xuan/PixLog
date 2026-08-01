# PixLog Runnable Demo

This directory builds a disposable Git repository inside the PixLog checkout.
It demonstrates visual diff, visual blame, AI-generation provenance, and policy
checks without creating a separate GitHub repository or changing the parent
repository's history.

## Run It

From the PixLog repository root:

```bash
brew install zhao-xuan/tap/pixlog chafa imagemagick
bash demo/setup.sh
cd demo/workspace
```

Then try the four core workflows:

```bash
pixlog diff HEAD~1 HEAD -- assets/hero.png
pixlog blame --point 760,145 assets/hero.png
pixlog recipe show --revision HEAD assets/ai-poster.png
bash scripts/apply-unsafe-edit.sh && pixlog check
```

The last command intentionally fails because the edit changes protected pixels,
exceeds the visual-change limit, and falls below the required SSIM. Rebuild the
workspace at any time with:

```bash
bash demo/setup.sh --force
```

The source images are checksum-pinned and licensed in
[`marketing/assets/sources/`](../marketing/assets/sources/README.md). The setup
uses real ImageMagick commands through `pixlog run`; it does not replay fixtures.