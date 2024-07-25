package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	instrument "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

var srvAddr = "0.0.0.0:8001"

var (
	scopeName    = ""
	serviceName  = ""
	metricName   = ""
	otelEndpoint = ""
	version      = "v0.1.0"
)

type ErrHandler struct {
	log *slog.Logger
}

func (h *ErrHandler) Handle(e error) {
	h.log.Error("OTEL", "error", e)
}

type Collector struct {
	m       *metric.MeterProvider
	counter instrument.Int64UpDownCounter
}

func CollectorNew(ctx context.Context, metricName string) (*Collector, error) {
	res, err := resource.Merge(resource.Default(),
		resource.NewWithAttributes(semconv.SchemaURL,
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion(version),
		))
	if err != nil {
		return nil, err
	}

	exp, err := otlpmetricgrpc.New(ctx,
		otlpmetricgrpc.WithInsecure(),
		otlpmetricgrpc.WithEndpoint(otelEndpoint),
	)
	if err != nil {
		return nil, err
	}

	m := metric.NewMeterProvider(
		metric.WithReader(metric.NewPeriodicReader(exp,
			metric.WithInterval(time.Second*10),
		)),
		metric.WithResource(res),
	)

	otel.SetMeterProvider(m)
	otel.SetErrorHandler(&ErrHandler{
		log: slog.Default(),
	})

	c := &Collector{
		m: m,
	}

	mt := m.Meter(scopeName)

	c.counter, err = mt.Int64UpDownCounter(metricName, instrument.WithUnit("count"))
	if err != nil {
		return nil, err
	}

	return c, nil
}

func (c *Collector) SendCount() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
	defer cancel()

	c.counter.Add(ctx, 1)
}

func (c *Collector) Shutdown(ctx context.Context) error {
	return c.m.Shutdown(ctx)
}

func main() {
	scopeName = os.Getenv("ENV_SCOPE_NAME")
	serviceName = os.Getenv("ENV_SERVICE_NAME")
	metricName = os.Getenv("ENV_METRIC_NAME")
	otelEndpoint = os.Getenv("ENV_OTEL_ENDPOINT")

	slog.Info("Print envs",
		slog.String("ENV_SCOPE_NAME", scopeName),
		slog.String("ENV_SERVICE_NAME", serviceName),
		slog.String("ENV_OTEL_ENDPOINT", otelEndpoint),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	t, err := CollectorNew(ctx, metricName)
	if err != nil {
		panic(err)
	}

	tick := time.NewTicker(time.Second * 10)
	defer tick.Stop()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				t.SendCount()
				slog.Info("Tick!")
			}
		}
	}()

	route := http.NewServeMux()

	route.HandleFunc("/api/v1/push", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		slog.Info("Request",
			slog.Any("request_headers", r.Header),
		)
		fmt.Fprintf(w, "Status code: %d", 200)
	})

	interrupt := make(chan os.Signal, 1)
	go signal.Notify(interrupt, syscall.SIGINT, syscall.SIGTERM)

	srv := &http.Server{
		Addr:    srvAddr,
		Handler: route,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil {
			slog.Error("Server failed", "error", err)
		}
	}()
	slog.Info("Server started", "serviceName", serviceName, "addr", srvAddr)
	<-interrupt

	// Attempt a graceful shutdown
	gctx, gcancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer gcancel()
	t.Shutdown(gctx)
}
