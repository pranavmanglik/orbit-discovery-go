# Architecture

`orbit-discovery-go` is the Go SDK for the versioned `orbit.discovery.v1` Protobuf contract. It
contains generated gRPC bindings and bounded client/server helpers. It is not a provider and does
not select Kubernetes, a registry backend, credentials, transport security, or a network listener.

```text
Python orbit-discovery contract source (versioned .proto)
     ├── Python ProcessServiceDiscovery bridge ↔ Core-supervised process host
     └── orbit-discovery-go
           ├── generated Go client/server bindings
           ├── discovery.NewClient(existing grpc.ClientConnInterface)
           └── discovery.Register(existing grpc.ServiceRegistrar, Resolver)
                    └── concrete provider, e.g. orbit-kubernetes-go
```

The canonical schema lives in `orbit-discovery/proto/orbit_discovery/v1`; this repository mirrors
it for Go generation. Update the mirror and generated files together with the owning contract
change. Compare the schemas before release and preserve the versioned package when adding
incompatible wire changes.

## Validation boundary

The package validates service and instance identifiers, host syntax, port, scheme, weight,
metadata, duplicate instance IDs, endpoint count, and serialized response size. It normalizes IP
and DNS host values, copies metadata, and maps provider failures to sanitized gRPC statuses. Client
`Resolve` checks local input and honors request context cancellation/deadlines.

Validation establishes syntactic bounds only. It does not verify DNS answers, endpoint reachability,
TLS identity, service health, or outbound network policy. The application must apply egress
controls before connecting to a returned endpoint.

For local selection from a validated snapshot, `WeightedPicker` implements smooth weighted
round-robin selection without network I/O. `Update` validates and atomically replaces the entire
snapshot; an invalid update leaves the previous snapshot intact. It has no health-probe or freshness
policy, which remains with the application.

## Ownership and security boundary

`Register` uses the supplied server registrar and does not configure or start the server. The host
application owns TLS, authentication, authorization, interceptors, inbound message limits,
concurrency limits, and server shutdown. `NewClient` wraps an existing gRPC connection but does not
own or close it; the caller owns credentials, TLS configuration, deadlines, connection reuse, and
connection shutdown.

Concrete provider adapters own vendor SDKs, identity, refresh loops, client resources, provider
timeouts, and provider error translation. The SDK has no provider dependency and no live registry
or Kubernetes behavior of its own.
