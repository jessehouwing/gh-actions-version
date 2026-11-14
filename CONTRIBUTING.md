# Contributing

1. Fork and branch from `main`.
2. Make changes with tests. Scenario tests in `internal/scenarios` use an in-memory fake `state.Client`.
3. Before opening a PR run:

```sh
gofmt -l .
go vet ./...
go test -race ./...
```

4. If you add or rename tests, regenerate the parity report: `pwsh scripts/test-parity.ps1 -WriteDoc`.

Keep Actions pinned by full commit SHA and workflow permissions minimal.
