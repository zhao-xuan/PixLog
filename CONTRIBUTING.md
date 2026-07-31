# Contributing to PixLog

## Development setup

PixLog requires Go 1.24 and Git. Clone the repository, download the Go modules,
and run the same checks used by CI:

```bash
go mod download
make check
```

Format changed Go files with `gofmt` before opening a pull request. Add focused
tests for behavioral changes and update the English and Chinese documentation
when a user-facing workflow changes.

## Pull requests

Keep each pull request focused on one problem. Describe the behavior change, the
reason for it, and the commands used to validate it. Do not commit generated
content from `bin/` or `dist/`.

All CI jobs must pass before merge. Changes to pointers, manifests, recipes, or
remote protocols should preserve backward compatibility or document the
migration explicitly.

## Releases

Maintainers publish releases from a clean, tested `main` branch by pushing an
annotated semantic version tag such as `v0.1.0`. The Release workflow builds and
publishes all supported archives; release binaries should not be committed to
the repository.