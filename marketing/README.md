# PixLog Marketing Pipeline

This directory contains reproducible source material for PixLog demos. It is
designed so an AI coding agent can change the narrative, execute the product,
record the terminal, and validate the resulting media without controlling a
desktop screen recorder.

## Architecture

1. `marketing/assets/sources/` contains checksum-pinned, licensed source media.
2. `demo/template/` is the tracked scaffold for the in-repository demo workspace.
3. `demo/setup.sh` runs real ImageMagick edits through `pixlog run` and creates CAS data.
4. `marketing/tapes/*.tape` declaratively controls the terminal with VHS.
5. `scripts/marketing/export-readme-assets.sh` renders the exact before/after states.
6. `scripts/marketing/record-demos.sh` refreshes those states, then renders GIF,
   MP4, and poster PNG outputs.

VHS is the control boundary: an AI edits a text tape instead of guessing mouse
coordinates. Hidden tape steps rebuild an isolated repository before every take;
visible steps only contain the user workflow. No network, credentials, or
desktop permissions are required during recording.

## Commands

```bash
brew install chafa imagemagick vhs ffmpeg
make marketing-record
```

Record one scenario while iterating:

```bash
bash scripts/marketing/record-demos.sh visual-history
```

Refresh only the README before/after images:

```bash
bash scripts/marketing/export-readme-assets.sh
```

Available scenarios:

- `visual-history`: stage, diff, heatmap, commit, and visual blame.
- `policy-check`: stage an unsafe edit and fail the visual policy.
- `visual-blame`: identify the commit that changed one coordinate.
- `ai-provenance`: inspect an image and reveal its generation recipe.

Build the runnable demo inside this repository:

```bash
make demo
cd demo/workspace
```

The generated workspace is ignored by the parent repository. It contains an
isolated five-commit history, ordinary source images, and local PixLog media
objects, so readers can exercise the complete workflow without another clone.

## AI Editing Contract

- Change narrative and timing in `marketing/tapes/*.tape`.
- Refresh source media only with `scripts/marketing/update-source-assets.sh`.
- Change ImageMagick regions, policy regions, and blame coordinates together.
- Keep setup commands between `Hide` and `Show`.
- Never put tokens, usernames, home paths, or remote endpoints in a tape.
- Keep each recording under 30 seconds and one idea per recording.
- Run the real command path before recording; do not replace output with fixtures.
- After rendering, check dimensions, duration, a middle frame, and a final frame.

Generated media lives under `docs/assets/demos/`. GIF is for GitHub and short
posts, MP4 is for social platforms, and PNG is the static fallback/cover image.
The exact image states shown beside the README recordings live under
`marketing/assets/demo/`.
