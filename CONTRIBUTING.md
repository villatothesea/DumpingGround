# Contributing

Issues and pull requests are welcome.

## Licensing of contributions

DumpingGround is currently licensed under the MIT License. By submitting a
contribution (code, documentation, or other material), you agree that:

1. Your contribution is licensed to the project under the MIT License, and
2. You grant the project maintainer the right to relicense the project —
   including your contribution — under the Apache License 2.0 in the future,
   without further permission or compensation.

This exists so the license can move MIT → Apache-2.0 later without tracking
down every past contributor. If you are not comfortable with that, please do
not submit code; bug reports and suggestions are still welcome and imply no
license grant.

## Ground rules

- This app deletes things. Changes that widen what gets deleted, weaken the
  confirmation/backup/recycle-bin safety layers, or silently fall back to
  permanent deletion will not be merged.
- `go test ./...` must pass before a PR is considered.
- Match the surrounding style; keep code compact and comments only where they
  earn their place.
