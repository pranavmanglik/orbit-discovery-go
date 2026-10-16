# Orbit Discovery Go: operations

`orbit-discovery-go` supplies generated Protobuf types plus a validating gRPC client/server wrapper.
It does not start a server, create a connection, load credentials, or select TLS settings. The
application owns those resources and passes an existing `grpc.ServiceRegistrar` to `Register` or
an existing `grpc.ClientConnInterface` to `NewClient`.

## Server integration

1. Create the gRPC server with the application's TLS, authentication, authorization, receive-size,
   concurrency, and observability policy.
2. Implement `discovery.Resolver`; honor the supplied context, return promptly on cancellation,
   and avoid provider error messages that contain credentials or personal data.
3. Register it with `discovery.Register(server, resolver)` and serve using the host application's
   lifecycle manager.
4. Stop accepting traffic and gracefully drain the gRPC server before closing resolver-owned
   provider clients.

`Register` rejects nil registrars and resolvers. The handler validates service identifiers and
every returned endpoint, enforces fan-out and encoded response limits, checks duplicate instance
IDs, and maps provider errors to generic statuses. Configure `grpc.MaxRecvMsgSize` and server
concurrency limits on the host server: the registration helper receives an already-created server
and cannot change its transport options.

## Client integration

Create a `grpc.ClientConn` with TLS and the application's workload credentials, then pass it to
`discovery.NewClient`. Reuse the connection according to gRPC's lifecycle guidance and let the
application own its shutdown. Call `Resolve` with a bounded deadline and a context that can be
cancelled when the request no longer needs the result. `Resolve` sets a per-call maximum receive
size of 1 MiB, so oversized protobuf messages are rejected by gRPC before the response is decoded.
The client does not own or close the connection and does not cache service results.

Validate endpoint egress policy at the point of use. DNS lookup and connection belong to the
consumer; this SDK only validates and normalizes the address syntax.

## Contract limits

- Service and instance identifiers: 128 ASCII bytes maximum.
- Hostnames: 253 bytes maximum; IP literals are canonicalized and DNS names use IDNA lookup rules.
- Port: 1–65,535; scheme: `http` or `https`; weight: 1–1,000.
- Metadata: at most 16 keys, 64-byte keys, 256 Unicode scalars per value, and 4 KiB total per
  instance.
- Fan-out: at most 4,096 instances; serialized response: at most 1 MiB.

These are contract-level bounds, not a substitute for host-level gRPC message, concurrency, and
deadline settings.

## Verification limits

Run `make test`, `make vet`, and `make build` for local validation. Tests use `bufconn`; they do not
exercise production TLS, identity, service-mesh policy, real registry availability, DNS/egress
policy, or cross-language release compatibility. Keep the Protobuf copy synchronized with the
Python capability contract and confirm generated bindings in the release workflow.
