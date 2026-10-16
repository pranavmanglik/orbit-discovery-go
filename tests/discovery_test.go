// Copyright 2026-present Orbit Contributors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package discovery_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/orbit-projects/orbit-discovery-go/discovery"
	discoveryv1 "github.com/orbit-projects/orbit-discovery-go/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type resolverFunc func(context.Context, string) ([]discovery.Instance, error)

type oversizedResponseServer struct {
	discoveryv1.UnimplementedServiceDiscoveryServer
}

func (oversizedResponseServer) Resolve(context.Context, *discoveryv1.ResolveRequest) (*discoveryv1.ResolveResponse, error) {
	return &discoveryv1.ResolveResponse{Instances: []*discoveryv1.ServiceInstance{{
		Service: "catalog", InstanceId: "catalog-1", Host: "127.0.0.1", Port: 8081,
		Scheme: "http", Weight: 1, Metadata: map[string]string{"payload": strings.Repeat("x", 2*discovery.MaxWireMessageBytes)},
	}}}, nil
}

func (f resolverFunc) Resolve(ctx context.Context, service string) ([]discovery.Instance, error) {
	return f(ctx, service)
}

func testClient(t *testing.T, resolver discovery.Resolver) *discovery.Client {
	t.Helper()
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	if err := discovery.Register(server, resolver); err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})

	connection, err := grpc.NewClient("passthrough:///orbit-discovery-test",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	client, err := discovery.NewClient(connection)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestRegisterAndResolveRoundTrip(t *testing.T) {
	client := testClient(t, resolverFunc(func(_ context.Context, name string) ([]discovery.Instance, error) {
		return []discovery.Instance{{Service: name, InstanceID: "catalog-1", Host: "127.0.0.1", Port: 8081,
			Scheme: "HTTP", Weight: 3, Metadata: map[string]string{"zone": "west"}}}, nil
	}))

	instances, err := client.Resolve(context.Background(), "catalog")
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 1 || instances[0].Scheme != "http" || instances[0].Metadata["zone"] != "west" {
		t.Fatalf("unexpected result: %#v", instances)
	}
	instances[0].Metadata["zone"] = "mutated"
	if instances[0].Metadata["zone"] != "mutated" {
		t.Fatal("client metadata should be caller-owned")
	}
}

func TestResolveNormalizesIDNAHostAndRejectsTypedNilResolver(t *testing.T) {
	var missing *resolverFunc
	if err := discovery.Register(grpc.NewServer(), missing); err == nil {
		t.Fatal("typed nil resolver must be rejected")
	}
	client := testClient(t, resolverFunc(func(_ context.Context, name string) ([]discovery.Instance, error) {
		return []discovery.Instance{{Service: name, InstanceID: "catalog-idna", Host: "BÜCHER.de.", Port: 443,
			Scheme: "https", Weight: 1}}, nil
	}))
	instances, err := client.Resolve(context.Background(), "catalog")
	if err != nil {
		t.Fatal(err)
	}
	if got := instances[0].Host; got != "xn--bcher-kva.de" {
		t.Fatalf("host should use canonical IDNA form: %q", got)
	}
}

func TestProviderFailuresAreSanitized(t *testing.T) {
	client := testClient(t, resolverFunc(func(context.Context, string) ([]discovery.Instance, error) {
		return nil, errors.New("credentials leaked: private-token")
	}))

	_, err := client.Resolve(context.Background(), "catalog")
	if status.Code(err) != codes.Internal || strings.Contains(err.Error(), "private-token") {
		t.Fatalf("provider failure was not sanitized: %v", err)
	}
}

func TestProviderResultsAreValidated(t *testing.T) {
	client := testClient(t, resolverFunc(func(_ context.Context, name string) ([]discovery.Instance, error) {
		return []discovery.Instance{{Service: name, InstanceID: "bad", Host: "user@host", Port: 80,
			Scheme: "http", Weight: 1}}, nil
	}))

	_, err := client.Resolve(context.Background(), "catalog")
	if status.Code(err) != codes.Internal {
		t.Fatalf("invalid provider data should fail closed: %v", err)
	}
}

func TestResponseFanoutAndWireSizeAreBounded(t *testing.T) {
	client := testClient(t, resolverFunc(func(_ context.Context, name string) ([]discovery.Instance, error) {
		instances := make([]discovery.Instance, 0, 300)
		metadata := make(map[string]string, 15)
		for index := 0; index < 15; index++ {
			metadata[fmt.Sprintf("key-%02d", index)] = strings.Repeat("x", 256)
		}
		for index := 0; index < 300; index++ {
			instances = append(instances, discovery.Instance{Service: name,
				InstanceID: fmt.Sprintf("endpoint-%04d", index), Host: "127.0.0.1", Port: 8081,
				Scheme: "http", Weight: 1, Metadata: metadata})
		}
		return instances, nil
	}))
	_, err := client.Resolve(context.Background(), "catalog")
	if status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("oversized response should fail with a bounded error: %v", err)
	}
}

func TestClientRejectsOversizedWireMessageBeforeDecoding(t *testing.T) {
	listener := bufconn.Listen(4 << 20)
	server := grpc.NewServer()
	server.RegisterService(&discoveryv1.ServiceDiscovery_ServiceDesc, oversizedResponseServer{})
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})
	connection, err := grpc.NewClient("passthrough:///orbit-discovery-oversized",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	client, err := discovery.NewClient(connection)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Resolve(context.Background(), "catalog")
	if status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("oversized response should be rejected by the gRPC receive bound: %v", err)
	}
}

func TestInvalidClientInputIsRejectedLocally(t *testing.T) {
	client, err := discovery.NewClient(nil)
	if err == nil || client != nil {
		t.Fatal("nil connection must be rejected")
	}
	var typedNilConnection *grpc.ClientConn
	client, err = discovery.NewClient(typedNilConnection)
	if err == nil || client != nil {
		t.Fatal("typed nil connection must be rejected")
	}

	client = testClient(t, resolverFunc(func(context.Context, string) ([]discovery.Instance, error) {
		return nil, nil
	}))
	_, err = client.Resolve(context.Background(), "invalid/name")
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("invalid service name should fail locally: %v", err)
	}
	_, err = client.Resolve(context.Background(), strings.Repeat("a", 1<<20))
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("oversized service name should fail locally: %v", err)
	}
}

func TestProviderMetadataIsRejectedBeforeOversizedWireResponse(t *testing.T) {
	client := testClient(t, resolverFunc(func(_ context.Context, name string) ([]discovery.Instance, error) {
		return []discovery.Instance{{Service: name, InstanceID: "catalog-1", Host: "127.0.0.1",
			Port: 8081, Scheme: "http", Weight: 1,
			Metadata: map[string]string{"unbounded": strings.Repeat("x", 1<<20)}}}, nil
	}))

	_, err := client.Resolve(context.Background(), "catalog")
	if status.Code(err) != codes.Internal {
		t.Fatalf("oversized provider metadata should fail closed: %v", err)
	}
}

func TestServerHonorsRequestCancellation(t *testing.T) {
	started := make(chan struct{})
	client := testClient(t, resolverFunc(func(ctx context.Context, _ string) ([]discovery.Instance, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := client.Resolve(ctx, "catalog")
		done <- err
	}()
	<-started
	cancel()
	select {
	case err := <-done:
		if status.Code(err) != codes.Canceled {
			t.Fatalf("cancellation should propagate unchanged: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled Resolve did not finish")
	}
}

func TestGeneratedContractServiceName(t *testing.T) {
	if got := discoveryv1.File_orbit_discovery_v1_service_discovery_proto.Services().Get(0).FullName(); got != "orbit.discovery.v1.ServiceDiscovery" {
		t.Fatalf("unexpected contract service name: %s", got)
	}
}
