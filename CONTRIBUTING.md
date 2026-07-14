# Contributing to golagram

Contributions are welcome. This is a small, single-maintainer project, so
please open an issue before starting anything nontrivial — it saves both of
us from a wasted PR.

## Building and testing

```sh
go build ./...
go vet ./...
gofmt -l .                 # should print nothing
go test ./... -race -count=1
```

Lint (same version CI runs):

```sh
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2
golangci-lint run ./...
```

Vulnerability scan:

```sh
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
```

## Regenerating the API surface

`types.gen.go`, `methods.gen.go`, and `consts.gen.go` are generated from
`scripts/api.json` (itself scraped from Telegram's docs by
`scripts/parse_botapi.py`) via `internal/gen`. Never hand-edit a `.gen.go`
file — CI's `codegen-check` job diffs a fresh regeneration against what's
committed and fails if they've drifted. After changing anything under
`internal/gen`, or after `scripts/api.json` is updated for a new Bot API
release:

```sh
go generate ./...
```

and commit the resulting diff alongside your change.

## Tests

- New code needs test coverage — CI enforces a statement-coverage floor
  that only ratchets upward.
- Prefer the pattern in `bot_test.go`'s `newTestBot`/`httptest.Server` for
  anything that talks to the Bot API — it fakes the HTTP layer instead of
  hitting real Telegram, so tests are deterministic and offline.
- `internal/gen` has a golden-output test (`testdata/mini_spec.json`,
  `-update` flag to regenerate the golden file when the generator's output
  intentionally changes).

## Style

- Idiomatic Go, stdlib-only at the root module (no third-party runtime
  deps) — this is a deliberate constraint, not an oversight; keep it that
  way unless you're discussing an exception in an issue first.
- Comments explain *why*, not what — see existing code for the tone.
- Run `golangci-lint run --fix ./...` before pushing if it flags anything
  autofixable.

## Reporting security issues

Do not open a public issue for a vulnerability — see
[SECURITY.md](SECURITY.md).

## License

By contributing, you agree your contribution is licensed under this
project's [MIT license](LICENSE).
