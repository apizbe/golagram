**What does this change and why?**

**Related issue** (if any):

**Checklist**

- [ ] `go build ./... && go vet ./... && gofmt -l .` clean
- [ ] `go test ./... -race -count=1` passes
- [ ] `golangci-lint run ./...` clean
- [ ] If this touches `internal/gen` or `scripts/api.json`: ran
      `go generate ./...` and committed the diff to `*.gen.go`
- [ ] Added/updated tests for the behavior this changes
- [ ] Updated README/godoc if this changes public API
