# Orbit Discovery Go: security

The Go package validates a provider-neutral endpoint contract. It is a gRPC contract library, not
a complete service-discovery deployment: it does not configure TLS, authenticate callers, authorize
service lookups, or impose network egress policy.

## Protect the gRPC boundary

Use authenticated, encrypted gRPC connections between trusted workloads. Configure TLS and
appropriate workload identity on the `grpc.Server` and `grpc.ClientConn` supplied by the host
application. Do not expose the registered service publicly without authentication, authorization,
and network policy. The test suite uses an in-memory connection with insecure credentials; that is
test transport only.

Limit which callers may resolve which service names. A valid response can reveal internal DNS
names, IP addresses, ports, zones, and metadata. The SDK sanitizes resolver failures and rejects
invalid or oversized responses, but it does not redact data returned by a successful lookup.

## Treat resolved endpoints as untrusted routing input

The SDK checks hostname syntax and canonicalizes IP literals and IDNA names. Syntax validation is
not an egress control. Before connecting to a resolved address, applications must enforce their
own destination policy to prevent access to metadata services, loopback, link-local, or otherwise
forbidden networks. Re-check policy after DNS resolution and on connection, since DNS answers can
change between validation and use.

Resolver implementations should return only authorized, healthy endpoints and bounded metadata.
Do not place secrets, personal data, bearer credentials, or internal configuration in metadata.
The SDK limits an identifier to 128 bytes, each result to 4,096 instances, metadata to 16 keys and
4 KiB total per instance, and each encoded response to 1 MiB.

## Failure handling

Provider errors are mapped to generic gRPC `Internal` status without forwarding provider text.
Invalid client input is returned as `InvalidArgument`; oversized results use `ResourceExhausted`;
malformed server responses use `DataLoss`. Log safe status codes and correlation data, not raw
resolver errors or sensitive endpoint metadata.

The package does not manage credentials or provider resources. Follow the repository
[vulnerability disclosure policy](../../SECURITY.md) for security reports.
