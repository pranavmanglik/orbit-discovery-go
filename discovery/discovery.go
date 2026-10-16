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

// Package discovery provides a Go API over Orbit's versioned service-discovery contract.
package discovery

import (
	"context"
	"fmt"
	"net"
	"reflect"
	"regexp"
	"strings"
	"unicode/utf8"

	discoveryv1 "github.com/orbit-projects/orbit-discovery-go/v1"
	"golang.org/x/net/idna"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const (
	// ResolveMethod is the typed RPC path used by the process-plugin capability protocol.
	ResolveMethod = "/orbit.discovery.v1.ServiceDiscovery/Resolve"
	// MaxInstancesPerResponse bounds endpoint fan-out for one lookup.
	MaxInstancesPerResponse = 4096
	maxMetadataKeys         = 16
	maxMetadataText         = 256
	maxMetadataSize         = 4096
	// MaxWireMessageBytes bounds encoded request and response messages.
	MaxWireMessageBytes = 1 << 20
)

var (
	identifierPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9._-]{0,126}[A-Za-z0-9])?$`)
	metadataPattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)
	dnsLabelPattern   = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
)

// Instance is a provider-neutral network address for one instance of a service.
type Instance struct {
	Service    string
	InstanceID string
	Host       string
	Port       uint32
	Scheme     string
	Weight     uint32
	Metadata   map[string]string
}

// Resolver returns the current candidate endpoints for a service name.
type Resolver interface {
	Resolve(context.Context, string) ([]Instance, error)
}

type grpcServer struct {
	discoveryv1.UnimplementedServiceDiscoveryServer
	resolver Resolver
}

// Register validates the resolver and registers the Orbit gRPC service on server.
// The host application owns server TLS, authentication, authorization, interceptors, and server
// lifecycle.
func Register(server grpc.ServiceRegistrar, resolver Resolver) error {
	if isNil(server) {
		return fmt.Errorf("gRPC service registrar is required")
	}
	if isNil(resolver) {
		return fmt.Errorf("service resolver is required")
	}
	discoveryv1.RegisterServiceDiscoveryServer(server, &grpcServer{resolver: resolver})
	return nil
}

// NewClient creates a typed client over an existing gRPC connection.
// The caller owns connection configuration and must close it when the application exits.
func NewClient(connection grpc.ClientConnInterface) (*Client, error) {
	if isNil(connection) {
		return nil, fmt.Errorf("gRPC client connection is required")
	}
	return &Client{client: discoveryv1.NewServiceDiscoveryClient(connection)}, nil
}

// Client calls Orbit's typed service-discovery contract.
type Client struct {
	client discoveryv1.ServiceDiscoveryClient
}

// Resolve returns a validated, detached endpoint snapshot for service.
// It rejects malformed service names locally and honors ctx cancellation and deadlines.
func (c *Client) Resolve(ctx context.Context, service string) ([]Instance, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("service-discovery client is not initialized")
	}
	if isNil(ctx) {
		return nil, fmt.Errorf("request context is required")
	}
	if err := validateIdentifier(service); err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid service identifier")
	}
	response, err := c.client.Resolve(
		ctx,
		&discoveryv1.ResolveRequest{Service: service},
		grpc.MaxCallRecvMsgSize(MaxWireMessageBytes),
	)
	if err != nil {
		return nil, err
	}
	if response == nil || proto.Size(response) > MaxWireMessageBytes {
		return nil, status.Error(codes.DataLoss, "service discovery returned an invalid response")
	}
	instances, err := decodeInstances(response.GetInstances(), service)
	if err != nil {
		return nil, status.Error(codes.DataLoss, "service discovery returned an invalid response")
	}
	return instances, nil
}

// Resolve implements the generated service and sanitizes provider failures before returning them.
func (s *grpcServer) Resolve(ctx context.Context, request *discoveryv1.ResolveRequest) (*discoveryv1.ResolveResponse, error) {
	if request == nil || validateIdentifier(request.GetService()) != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid service identifier")
	}
	instances, err := s.resolver.Resolve(ctx, request.GetService())
	if err != nil {
		if ctx.Err() != nil {
			return nil, status.FromContextError(ctx.Err()).Err()
		}
		return nil, status.Error(codes.Internal, "service discovery provider failed")
	}
	response := &discoveryv1.ResolveResponse{Instances: make([]*discoveryv1.ServiceInstance, 0, len(instances))}
	if len(instances) > MaxInstancesPerResponse {
		return nil, status.Error(codes.ResourceExhausted, "service discovery result exceeds endpoint limit")
	}
	seen := make(map[string]struct{}, len(instances))
	for _, instance := range instances {
		if err := validateInstance(instance, request.GetService()); err != nil {
			return nil, status.Error(codes.Internal, "service discovery provider returned an invalid endpoint")
		}
		if _, duplicate := seen[instance.InstanceID]; duplicate {
			return nil, status.Error(codes.Internal, "service discovery provider returned duplicate endpoints")
		}
		seen[instance.InstanceID] = struct{}{}
		response.Instances = append(response.Instances, encodeInstance(instance))
	}
	if proto.Size(response) > MaxWireMessageBytes {
		return nil, status.Error(codes.ResourceExhausted, "service discovery result exceeds message limit")
	}
	return response, nil
}

func decodeInstances(values []*discoveryv1.ServiceInstance, service string) ([]Instance, error) {
	if len(values) > MaxInstancesPerResponse {
		return nil, fmt.Errorf("endpoint limit exceeded")
	}
	result := make([]Instance, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value == nil {
			return nil, fmt.Errorf("nil endpoint")
		}
		instance := Instance{
			Service: value.GetService(), InstanceID: value.GetInstanceId(), Host: value.GetHost(),
			Port: value.GetPort(), Scheme: value.GetScheme(), Weight: value.GetWeight(),
			Metadata: value.GetMetadata(),
		}
		if err := validateInstance(instance, service); err != nil {
			return nil, err
		}
		instance.Host, _ = normalizeHost(instance.Host)
		if _, duplicate := seen[instance.InstanceID]; duplicate {
			return nil, fmt.Errorf("duplicate endpoint identifier")
		}
		seen[instance.InstanceID] = struct{}{}
		result = append(result, cloneInstance(instance))
	}
	return result, nil
}

func encodeInstance(value Instance) *discoveryv1.ServiceInstance {
	host, _ := normalizeHost(value.Host)
	return &discoveryv1.ServiceInstance{
		Service: value.Service, InstanceId: value.InstanceID, Host: host,
		Port: value.Port, Scheme: strings.ToLower(value.Scheme), Weight: value.Weight,
		Metadata: cloneMap(value.Metadata),
	}
}

func validateInstance(value Instance, service string) error {
	if validateIdentifier(value.Service) != nil || value.Service != service || validateIdentifier(value.InstanceID) != nil {
		return fmt.Errorf("invalid service or endpoint identifier")
	}
	if _, valid := normalizeHost(value.Host); !valid || value.Port == 0 || value.Port > 65535 {
		return fmt.Errorf("invalid endpoint address")
	}
	if !strings.EqualFold(value.Scheme, "http") && !strings.EqualFold(value.Scheme, "https") {
		return fmt.Errorf("invalid endpoint scheme")
	}
	if value.Weight == 0 || value.Weight > 1000 {
		return fmt.Errorf("invalid endpoint weight")
	}
	if len(value.Metadata) > maxMetadataKeys {
		return fmt.Errorf("metadata entry limit exceeded")
	}
	metadataBytes := 0
	for key, item := range value.Metadata {
		// Check byte limits before regex and UTF-8 scans. Provider data can be unexpectedly large;
		// bounded scans keep validation cost proportional to the public metadata contract.
		if len(key) > 64 || len(item) > maxMetadataText*utf8.UTFMax {
			return fmt.Errorf("metadata size limit exceeded")
		}
		metadataBytes += len(key) + len(item)
		if metadataBytes > maxMetadataSize {
			return fmt.Errorf("metadata size limit exceeded")
		}
		if !metadataPattern.MatchString(key) || !utf8.ValidString(item) || utf8.RuneCountInString(item) > maxMetadataText {
			return fmt.Errorf("invalid metadata")
		}
		for _, char := range item {
			if char < 32 || char == 127 {
				return fmt.Errorf("invalid metadata")
			}
		}
	}
	return nil
}

func validateIdentifier(value string) error {
	if len(value) > 128 || !identifierPattern.MatchString(value) {
		return fmt.Errorf("invalid service identifier")
	}
	return nil
}

func normalizeHost(value string) (string, bool) {
	if value == "" || len(value) > 253 || strings.TrimSpace(value) != value || strings.ContainsAny(value, `/\\@?#%`) {
		return "", false
	}
	for _, char := range value {
		if char < 33 || char == 127 {
			return "", false
		}
	}
	if address := net.ParseIP(value); address != nil {
		return address.String(), true
	}
	host, err := idna.Lookup.ToASCII(strings.TrimSuffix(value, "."))
	if err != nil {
		return "", false
	}
	host = strings.ToLower(host)
	if host == "" {
		return "", false
	}
	for _, label := range strings.Split(host, ".") {
		if !dnsLabelPattern.MatchString(label) {
			return "", false
		}
	}
	return host, true
}

func isNil(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

func cloneInstance(value Instance) Instance {
	value.Metadata = cloneMap(value.Metadata)
	return value
}

func cloneMap(value map[string]string) map[string]string {
	if len(value) == 0 {
		return nil
	}
	result := make(map[string]string, len(value))
	for key, item := range value {
		result[key] = item
	}
	return result
}
