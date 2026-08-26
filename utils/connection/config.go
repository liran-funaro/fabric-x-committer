/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package connection

import (
	"crypto/tls"

	"google.golang.org/grpc/credentials"

	"github.com/hyperledger/fabric-x-committer/utils/retry"
)

type (
	// MultiClientConfig contains the endpoints, TLS config, and retry profile.
	// This config allows the support of number of different endpoints to multiple service instances.
	MultiClientConfig struct {
		Endpoints   []*Endpoint       `mapstructure:"endpoints"`
		TLS         TLSConfig         `mapstructure:"tls"`
		Retry       *retry.Profile    `mapstructure:"reconnect"`
		FlowControl FlowControlConfig `mapstructure:"flow-control"`
	}

	// ClientConfig contains a single endpoint, TLS config, and retry profile.
	ClientConfig struct {
		Endpoint    *Endpoint         `mapstructure:"endpoint"`
		TLS         TLSConfig         `mapstructure:"tls"`
		Retry       *retry.Profile    `mapstructure:"reconnect"`
		FlowControl FlowControlConfig `mapstructure:"flow-control"`
	}

	// FlowControlConfig sizes the HTTP/2 flow control windows of a connection. It is per client and
	// per server because the right size depends on the message sizes and the round-trip time of the
	// link, which differ between peers.
	//
	// The recommended values are applied when a field is left unset, rather than through a `default`
	// struct tag. A tag would register a viper default for a key nested inside
	// ClientConfig, and ClientConfig is reachable through optional pointer fields whose nil-ness is
	// semantic -- the load generator selects its adapter by which client section is present. A
	// registered default under such a pointer materialises it, so the tag would silently change
	// adapter selection.
	//
	// The recommended values are what was measured to remove the stall described below, not a guess.
	// gRPC's
	// own defaults were the ceiling of a nineteen-machine deployment: at 500,000 transactions per
	// second every one of the coordinator's senders to the signature verifiers, and five of its six
	// to the validator-committers, sat blocked in the transport's writeQuota while using a quarter of
	// a core each, and the verifiers they feed ran 22 of 64 cores. Raising these took the pipeline
	// from 510,371 to 578,383 transactions per second and its latency at a sustainable 500,000 from
	// 645 ms to 392 ms.
	//
	// These are credit limits rather than allocations, so the cost is bounded buffering per
	// connection and only under overload; at a sustainable rate the deployment above held 230,000
	// transactions in flight, which was its own backpressure window and essentially nothing else.
	FlowControlConfig struct {
		// InitialWindowSize is the per-stream window in bytes. Unset means
		// RecommendedInitialWindowSize; negative means leave gRPC's own BDP-based window tuning in
		// place, which any explicit value disables.
		InitialWindowSize int32 `mapstructure:"initial-window-size"`
		// InitialConnWindowSize is the connection-level counterpart, shared by the streams on a
		// connection, so it should exceed InitialWindowSize. Unset and negative mean the same as above.
		InitialConnWindowSize int32 `mapstructure:"initial-conn-window-size"`
	}

	// TLSConfig holds the TLS options and certificate paths
	// used for secure communication between servers and clients.
	// Credentials are built based on the configuration mode.
	// For example, If only server-side TLS is required, the certificate pool (certPool) is not built (for a server),
	// since the relevant certificates paths are defined in the YAML according to the selected mode.
	TLSConfig struct {
		Mode string `mapstructure:"mode" validate:"omitempty,oneof=tls mtls none"`
		// CertPath is the path to the certificate file (public key).
		CertPath string `mapstructure:"cert-path"`
		// KeyPath is the path to the key file (private key).
		KeyPath     string   `mapstructure:"key-path"`
		CACertPaths []string `mapstructure:"ca-cert-paths"`
	}
)

// usage: TLS configuration modes.
const (
	UnmentionedTLSMode = ""
	NoneTLSMode        = "none"
	OneSideTLSMode     = "tls"
	MutualTLSMode      = "mtls"
	DefaultTLSMode     = NoneTLSMode

	// DefaultTLSMinVersion is the minimum version required to achieve secure connections.
	DefaultTLSMinVersion = tls.VersionTLS12
)

// ClientCredentials converts TLSConfig into a TLSCredentials struct and generates client creds.
func (c TLSConfig) ClientCredentials() (credentials.TransportCredentials, error) {
	tlsCreds, err := NewClientTLSCredentials(c)
	if err != nil {
		return nil, err
	}
	return NewClientGRPCTransportCredentials(tlsCreds)
}

// ServerCredentials converts TLSConfig into a TLSCredentials struct and generates server creds.
func (c TLSConfig) ServerCredentials() (credentials.TransportCredentials, error) {
	tlsCreds, err := NewServerTLSCredentials(c)
	if err != nil {
		return nil, err
	}
	return NewServerGRPCTransportCredentials(tlsCreds)
}

// StreamWindow returns the per-stream HTTP/2 window to apply, or zero to apply none and leave
// gRPC's own BDP-based tuning in place.
func (c FlowControlConfig) StreamWindow() int32 {
	return resolveWindow(c.InitialWindowSize, RecommendedInitialWindowSize)
}

// ConnWindow returns the connection-level HTTP/2 window to apply, or zero to apply none.
func (c FlowControlConfig) ConnWindow() int32 {
	return resolveWindow(c.InitialConnWindowSize, RecommendedInitialConnWindowSize)
}

func resolveWindow(configured int32, recommended int) int32 {
	switch {
	case configured == 0:
		return int32(recommended) //nolint:gosec // both recommended values are well inside int32.
	case configured < 0:
		return 0
	default:
		return configured
	}
}
