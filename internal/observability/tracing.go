package observability

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

func InitTracing(ctx context.Context, endpoint, service string) (func(context.Context) error, error) {
	options := []sdktrace.TracerProviderOption{sdktrace.WithResource(resource.NewSchemaless(attribute.String("service.name", service)))}
	if endpoint != "" {
		exporter, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(endpoint))
		if err != nil {
			return nil, err
		}
		options = append(options, sdktrace.WithBatcher(exporter))
	}
	provider := sdktrace.NewTracerProvider(options...)
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	return provider.Shutdown, nil
}

type traceResponseWriter struct {
	http.ResponseWriter
	status int
}

func (w *traceResponseWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
func (w *traceResponseWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	return w.ResponseWriter.Write(body)
}
func TraceMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))
		ctx, span := otel.Tracer("fccp/http").Start(ctx, r.Method+" "+r.URL.Path, trace.WithSpanKind(trace.SpanKindServer), trace.WithAttributes(attribute.String("http.request.method", r.Method), attribute.String("url.path", r.URL.Path)))
		defer span.End()
		wrapped := &traceResponseWriter{ResponseWriter: w}
		r = r.WithContext(ctx)
		next.ServeHTTP(wrapped, r)
		if r.Pattern != "" {
			span.SetName(r.Pattern)
			span.SetAttributes(attribute.String("http.route", r.Pattern))
		}
		status := wrapped.status
		if status == 0 {
			status = 200
		}
		span.SetAttributes(attribute.Int("http.response.status_code", status))
		if status >= 500 {
			span.SetStatus(codes.Error, strconv.Itoa(status))
		}
	})
}
func TraceIDs(ctx context.Context) (string, string) {
	span := trace.SpanFromContext(ctx).SpanContext()
	if !span.IsValid() {
		return "", ""
	}
	return span.TraceID().String(), span.SpanID().String()
}

type PGXTracer struct{}

func (PGXTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	ctx, span := otel.Tracer("fccp/postgresql").Start(ctx, "postgresql.query", trace.WithSpanKind(trace.SpanKindClient), trace.WithAttributes(attribute.String("db.system", "postgresql"), attribute.String("db.operation.name", queryOperation(data.SQL))))
	return context.WithValue(ctx, querySpanKey{}, span)
}
func (PGXTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	span, _ := ctx.Value(querySpanKey{}).(trace.Span)
	if span == nil {
		return
	}
	if data.Err != nil {
		span.RecordError(data.Err)
		span.SetStatus(codes.Error, data.Err.Error())
	}
	span.End()
}

type querySpanKey struct{}

func queryOperation(sql string) string {
	for i, c := range sql {
		if c == ' ' || c == '\n' || c == '\t' {
			if i > 0 {
				return sql[:i]
			}
		}
	}
	if sql == "" {
		return "unknown"
	}
	return sql
}

func StartWorkerSpan(ctx context.Context, name string) (context.Context, trace.Span) {
	return otel.Tracer("fccp/workers").Start(ctx, name, trace.WithSpanKind(trace.SpanKindInternal), trace.WithTimestamp(time.Now()))
}
