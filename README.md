# Orbit Discovery Go Contract Library

`orbit-discovery-go` contains the generated Go bindings and bounded client/server helpers for the
provider-neutral `orbit.discovery.v1.ServiceDiscovery` contract. It helps a Go implementation
conform to the same capability API used by Python `orbit-discovery`; it is not itself a concrete
service-discovery provider. For example, `orbit-kubernetes-go` uses this contract library and
implements Kubernetes EndpointSlice discovery in Go. Concrete providers own their vendor SDKs,
credentials, configuration, transport, lifecycle, and provider failures.

The versioned Protobuf schema is mirrored from `orbit-discovery/proto/orbit_discovery/v1`. The
Python capability package owns the contract definition; keep this copy and generated bindings in
sync when that contract changes. Generated types, client, server interface, and registration helpers
are public under `github.com/orbit-projects/orbit-discovery-go/v1`.

```go
import (
	"context"

	"github.com/orbit-projects/orbit-discovery-go/discovery"
	"google.golang.org/grpc"
)

type catalogResolver struct{}

func (catalogResolver) Resolve(_ context.Context, service string) ([]discovery.Instance, error) {
	if service != "catalog" {
		return nil, nil
	}
	return []discovery.Instance{{
		Service: "catalog", InstanceID: "catalog-1", Host: "127.0.0.1",
		Port: 8081, Scheme: "http", Weight: 1,
	}}, nil
}

func register(server *grpc.Server) error {
	return discovery.Register(server, catalogResolver{})
}
```

`discovery.Register` validates requests and provider results against the language-neutral bounded
contract, returns sanitized gRPC errors, and honors context cancellation. `discovery.NewClient`
offers the matching Go client API. The package limits each result to 4,096 endpoints and each wire
message to 1 MiB, and bounds endpoint identifiers, host syntax, ports, weights, and metadata. The
SDK validates endpoint syntax; callers still need egress policy to prevent SSRF when connecting to
resolved hosts.

The SDK also includes `WeightedPicker` for request-local endpoint selection. It uses smooth weighted
round robin without network calls, snapshots and validates each update, returns detached endpoint
metadata, and reports `ErrNoInstances` when a refreshed service has no
ready endpoints. Refresh the snapshot from `Client.Resolve` according to the application's freshness
policy; the picker does not cache, probe, or declare endpoints healthy.

```go
import (
    "errors"

    "github.com/orbit-projects/orbit-discovery-go/discovery"
)

instances, err := client.Resolve(ctx, "catalog")
if err != nil {
    return err
}
picker, err := discovery.NewWeightedPicker("catalog", instances)
if err != nil {
    return err
}
backend, err := picker.Pick()
if errors.Is(err, discovery.ErrNoInstances) {
    // Return the application's service-unavailable response.
} else if err != nil {
    return err
}
_ = backend // Use the endpoint with the application's connection pool.
```

Call `picker.Update` after refreshing the discovery snapshot. The application still owns connection
pools, health checks, retry safety, and stale-endpoint behavior.

## Documentation

The package-specific guides cover [architecture](docs/architecture/overview.md), [operations and security](docs/operations/README.md), and [development](docs/development/README.md), with [security guidance](docs/security/overview.md). The [documentation index](docs/README.md) links to the full package overview and project policies.

## Development

Requires Go 1.24 or later and `protoc` for regeneration. Generated Go files are checked in, so
consumers do not need the compiler.

```sh
make tools
make generate
make test
make vet
make build
```

Tests use an in-memory gRPC transport. They do not verify a live registry, a Kubernetes cluster,
Python Core process hosting, or release provenance. This package is in alpha and is not a
production certification.

Licensed under Apache-2.0.

