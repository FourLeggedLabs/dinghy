/*
* Copyright 2026 Four Legged Labs
*
* Licensed under the Apache License, Version 2.0 (the "License");
* you may not use this file except in compliance with the License.
* You may obtain a copy of the License at
*
*    http://www.apache.org/licenses/LICENSE-2.0
*
* Unless required by applicable law or agreed to in writing, software
* distributed under the License is distributed on an "AS IS" BASIS,
* WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
* See the License for the specific language governing permissions and
* limitations under the License.
 */

// Package otel provides OpenTelemetry initialization for dinghy: a tracer
// provider with OTLP export, W3C trace context propagation, and helpers for
// instrumenting the webhook processing pipeline.
package otel

import (
	"context"
	"fmt"
	"go.opentelemetry.io/otel/codes"
	"os"
	"strings"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	"net/http"
)

const (
	instrumentationName = "github.com/fourleggedlabs/dinghy"

	envEndpoint  = "OTEL_EXPORTER_OTLP_ENDPOINT"
	envProtocol  = "OTEL_EXPORTER_OTLP_PROTOCOL"
	envInsecure  = "OTEL_EXPORTER_OTLP_INSECURE"
	envSamplePct = "OTEL_TRACES_SAMPLER_ARG"

	envServiceName = "OTEL_SERVICE_NAME"
)

// Config controls how the tracer provider is initialized.
type Config struct {
	// Enabled turns on OTLP trace export. Defaults to true when
	// OTEL_EXPORTER_OTLP_ENDPOINT is set.
	Enabled bool
	// ServiceName reported in spans. Defaults to "dinghy".
	ServiceName string
	// Endpoint for the OTLP collector, e.g. "otel-collector:4317".
	Endpoint string
	// Protocol: "grpc" (default) or "http/protobuf".
	Protocol string
	// Insecure disables TLS for the exporter (default: true for grpc on
	// non-4317 ports is collector-dependent; here we default to insecure
	// for in-cluster collectors, set OTEL_EXPORTER_OTLP_INSECURE=false for TLS).
	Insecure bool
	// SampleRatio between 0 and 1. Defaults to 1.0 (always sample).
	SampleRatio float64
}

// ConfigFromEnv builds a Config from standard OTEL_* environment variables.
// Tracing is enabled when OTEL_EXPORTER_OTLP_ENDPOINT is set.
func ConfigFromEnv() Config {
	cfg := Config{
		ServiceName: "dinghy",
		Protocol:    "grpc",
		Insecure:    true,
		SampleRatio: 1.0,
	}
	if v := os.Getenv(envServiceName); v != "" {
		cfg.ServiceName = v
	}
	if v := os.Getenv(envEndpoint); v != "" {
		cfg.Enabled = true
		cfg.Endpoint = v
	}
	if v := os.Getenv(envProtocol); v != "" {
		cfg.Protocol = v
	}
	if v := os.Getenv(envInsecure); v == "false" {
		cfg.Insecure = false
	}
	if v := os.Getenv(envSamplePct); v != "" {
		var r float64
		if _, err := fmt.Sscanf(v, "%g", &r); err == nil && r >= 0 && r <= 1 {
			cfg.SampleRatio = r
		}
	}
	return cfg
}

// InitTracer initializes the global tracer provider, propagator and returns
// a shutdown function. It is safe to call when tracing is disabled — in that
// case a no-op shutdown is returned and the global propagator is still set
// so trace context from upstream (e.g. Spinnaker) is honored.
func InitTracer(ctx context.Context, cfg Config) (func(context.Context) error, error) {
	// Always install the W3C propagator so incoming traceparent headers
	// are extracted and outgoing requests propagate context.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	if !cfg.Enabled {
		return func(context.Context) error { return nil }, nil
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceNamespace("fourleggedlabs"),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("otel resource: %w", err)
	}

	exp, err := newExporter(ctx, cfg)
	if err != nil {
		return nil, err
	}

	opts := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(res),
		sdktrace.WithBatcher(exp),
	}
	if cfg.SampleRatio < 1.0 {
		opts = append(opts, sdktrace.WithSampler(sdktrace.TraceIDRatioBased(cfg.SampleRatio)))
	}
	tp := sdktrace.NewTracerProvider(opts...)
	otel.SetTracerProvider(tp)

	return tp.Shutdown, nil
}

func newExporter(ctx context.Context, cfg Config) (sdktrace.SpanExporter, error) {
	switch strings.ToLower(cfg.Protocol) {
	case "http/protobuf", "http":
		var opts []otlptracehttp.Option
		if cfg.Endpoint != "" {
			opts = append(opts, otlptracehttp.WithEndpoint(strings.TrimPrefix(cfg.Endpoint, "http://")))
		}
		if cfg.Insecure {
			opts = append(opts, otlptracehttp.WithInsecure())
		}
		return otlptracehttp.New(ctx, opts...)
	case "grpc", "":
		var opts []otlptracegrpc.Option
		if cfg.Endpoint != "" {
			opts = append(opts, otlptracegrpc.WithEndpoint(cfg.Endpoint))
		}
		if cfg.Insecure {
			opts = append(opts, otlptracegrpc.WithInsecure())
		}
		return otlptracegrpc.New(ctx, opts...)
	default:
		return nil, fmt.Errorf("unsupported OTEL protocol: %s (use grpc or http/protobuf)", cfg.Protocol)
	}
}

// Tracer is the shared tracer for dinghy instrumentation.
var Tracer = otel.Tracer(instrumentationName)

// Handler wraps an http.Handler with otelhttp instrumentation, creating a
// server span per request with HTTP attributes. When tracing is disabled the
// middleware is still safe: it extracts propagates context with negligible
// overhead and creates non-exported spans on a noop provider.
func Handler(next http.Handler) http.Handler {
	return otelhttp.NewHandler(next, "dinghy",
		otelhttp.WithTracerProvider(otel.GetTracerProvider()),
		otelhttp.WithPropagators(otel.GetTextMapPropagator()))
}

// StartSpan starts a span with the given name, attaching it to the context.
// Example:
//
//	ctx, span := otel.StartSpan(ctx, "processDinghyfile")
//	defer span.End()
func StartSpan(ctx context.Context, name string) (context.Context, trace.Span) {
	return Tracer.Start(ctx, name)
}

// RecordError marks a span as failed with the error and an error event.
func RecordError(span trace.Span, err error) {
	if err == nil {
		return
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}
