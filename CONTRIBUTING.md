# Contributing

Orbit Discovery Go is an Apache-2.0 licensed SDK in alpha review. Contributions should preserve the
versioned Protobuf service and the limits documented in the root README. Changes to request/response
semantics must be coordinated with the `orbit-discovery` capability repository and its Python
bindings.

Before sending a change, run:

```sh
make test
make vet
make build
```

For schema or generated-code changes, also run `make generate` and include the generated bindings.
Do not include provider credentials, private endpoint data, or environment-specific test output in
issues or changes. Report security vulnerabilities privately as described in `SECURITY.md`.


## Package documentation

Use the [architecture](docs/architecture/overview.md), [operations](docs/operations/README.md), and [development](docs/development/README.md) guides when changing package contracts or operator behavior.
