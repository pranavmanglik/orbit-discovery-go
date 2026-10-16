# Orbit Discovery Go Contract Library: development

Use the checks for each language manifest present in this repository:

### Go package

```bash
go test ./...
go test -race ./...
go vet ./...
gofmt -d $(git ls-files '*.go')
go build ./...
```

Optional SDKs and live-service tests are described by the package README. Do not run an opt-in live test against a shared or production service unless its own documentation explicitly supports that use.

## Protobuf generation

The generated Go bindings are verified in CI. Keep the generation toolchain aligned with the workflow:
`protoc` 36.1, `protoc-gen-go` 1.36.6, and `protoc-gen-go-grpc` 1.5.1. After changing the `.proto`
contract, run:

```bash
make tools
make generate
git diff -- v1
```

Review and include generated `v1` files with the contract change. CI regenerates the bindings and
requires that the generated tree has no differences. The license-header script is part of
`make generate` so generated source keeps the repository's Apache-2.0 notice.

On 2026-10-10, generation was reproducible on Go 1.27.1, and `go test -race ./...`, `go vet ./...`,
`go build -trimpath ./...`, and `gofmt` checks passed. Go 1.24 CI compatibility and hosted rerun
remain separate gates.

## Contribution expectations

Add focused regression coverage for behavior changes. Update the README and relevant guide when a contract, configuration option, security property, lifecycle rule, or operational assumption changes. Report the exact versions and services exercised; local checks do not imply hosted CI or release validation.
