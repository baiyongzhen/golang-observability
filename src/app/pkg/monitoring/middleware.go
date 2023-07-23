package monitoring

// https://github.com/open-telemetry/opentelemetry-go-contrib/blob/main/instrumentation/github.com/gorilla/mux/otelmux/mux.go#L39
import (
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/felixge/httpsnoop"
	"github.com/gorilla/mux"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
	"go.opentelemetry.io/otel/semconv/v1.17.0/httpconv"
	"go.opentelemetry.io/otel/trace"
	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/prometheus/client_golang/prometheus"

	"otel-golang-observability/pkg/util"
)

const (
	tracerName = "go.opentelemetry.io/contrib/instrumentation/github.com/gorilla/mux/otelmux"
)

var INFO = prometheus.NewGaugeVec(prometheus.GaugeOpts{
	Name:      "app_info",
	Help:      "application information.",
	}, 
	[]string{"app_name"},
)

var REQUESTS = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Total count of requests by method and path.",
	},
	[]string{"method", "path", "app_name"},
)

var RESPONSES = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "http_responses_total",
		Help: "Total count of responses by method, path and status codes.",
	},
	[]string{"method", "path", "status_code", "app_name"},
)

var REQUESTS_PROCESSING_TIME = prometheus.NewHistogramVec(prometheus.HistogramOpts{
	Name: "http_requests_duration_seconds",
	Help: "Histogram of requests processing time by path (in seconds)",
}, []string{"method", "path", "app_name"})

var REQUESTS_PROCESSING_TIME_EXEMPLAR = prometheus.NewHistogram(prometheus.HistogramOpts{
	Name: "http_requests_duration_seconds_exemplar",
	Help: "Histogram of requests processing time by path (in seconds)",
	//Buckets: prometheus.ExponentialBuckets(0.1, 1.5, 5),
	//Buckets:   append(prometheus.DefBuckets, 30, 60),
})

var EXCEPTIONS = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "http_exceptions_total",
		Help: "Total count of exceptions raised by path and exception type",
	},
	[]string{"method", "path", "exception_type", "app_name"},
)

var REQUESTS_IN_PROGRESS = prometheus.NewGaugeVec(prometheus.GaugeOpts{
	Name:      "http_requests_in_progress",
	Help:      "Gauge of requests by method and path currently being processed",
}, []string{"method", "path", "app_name"})

func init() {
	prometheus.Register(INFO)
	prometheus.Register(REQUESTS)
	prometheus.Register(RESPONSES)
	prometheus.Register(REQUESTS_PROCESSING_TIME)
	prometheus.Register(REQUESTS_PROCESSING_TIME_EXEMPLAR)
	prometheus.Register(EXCEPTIONS)
	prometheus.Register(REQUESTS_IN_PROGRESS)

	INFO.With(prometheus.Labels{
		"app_name": util.GetEnv("APP_NAME", "app"),
		}).Inc()
}

// config is used to configure the mux middleware.
type config struct {
	TracerProvider    oteltrace.TracerProvider
	Propagators       propagation.TextMapPropagator
	spanNameFormatter func(string, *http.Request) string
	PublicEndpoint    bool
	PublicEndpointFn  func(*http.Request) bool
}

// Option specifies instrumentation configuration options.
type Option interface {
	apply(*config)
}

// Version is the current release version of the gorilla/mux instrumentation.
func Version() string {
	return "0.42.0"
	// This string is updated by the pre_release.sh script during release
}

// Middleware sets up a handler to start tracing the incoming
// requests.  The service parameter should describe the name of the
// (virtual) server handling the request.
func RouteMiddleware(service string, opts ...Option) mux.MiddlewareFunc {
	cfg := config{}
	for _, opt := range opts {
		opt.apply(&cfg)
	}
	if cfg.TracerProvider == nil {
		cfg.TracerProvider = otel.GetTracerProvider()
	}
	tracer := cfg.TracerProvider.Tracer(
		tracerName,
		trace.WithInstrumentationVersion(Version()),
	)
	if cfg.Propagators == nil {
		cfg.Propagators = otel.GetTextMapPropagator()
	}
	if cfg.spanNameFormatter == nil {
		cfg.spanNameFormatter = defaultSpanNameFunc
	}

	return func(handler http.Handler) http.Handler {
		return traceware{
			service:           service,
			tracer:            tracer,
			propagators:       cfg.Propagators,
			handler:           handler,
			spanNameFormatter: cfg.spanNameFormatter,
			publicEndpoint:    cfg.PublicEndpoint,
			publicEndpointFn:  cfg.PublicEndpointFn,
		}
	}
}

type traceware struct {
	service           string
	tracer            trace.Tracer
	propagators       propagation.TextMapPropagator
	handler           http.Handler
	spanNameFormatter func(string, *http.Request) string
	publicEndpoint    bool
	publicEndpointFn  func(*http.Request) bool
}

type recordingResponseWriter struct {
	writer  http.ResponseWriter
	written bool
	status  int
}

var rrwPool = &sync.Pool{
	New: func() interface{} {
		return &recordingResponseWriter{}
	},
}

func getRRW(writer http.ResponseWriter) *recordingResponseWriter {
	rrw := rrwPool.Get().(*recordingResponseWriter)
	rrw.written = false
	rrw.status = http.StatusOK
	rrw.writer = httpsnoop.Wrap(writer, httpsnoop.Hooks{
		Write: func(next httpsnoop.WriteFunc) httpsnoop.WriteFunc {
			return func(b []byte) (int, error) {
				if !rrw.written {
					rrw.written = true
				}
				return next(b)
			}
		},
		WriteHeader: func(next httpsnoop.WriteHeaderFunc) httpsnoop.WriteHeaderFunc {
			return func(statusCode int) {
				if !rrw.written {
					rrw.written = true
					rrw.status = statusCode
				}
				next(statusCode)
			}
		},
	})
	return rrw
}

func putRRW(rrw *recordingResponseWriter) {
	rrw.writer = nil
	rrwPool.Put(rrw)
}

// defaultSpanNameFunc just reuses the route name as the span name.
func defaultSpanNameFunc(routeName string, _ *http.Request) string { return routeName }

// ServeHTTP implements the http.Handler interface. It does the actual
// tracing of the request.
func (tw traceware) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := tw.propagators.Extract(r.Context(), propagation.HeaderCarrier(r.Header))
	routeStr := ""
	route := mux.CurrentRoute(r)
	if route != nil {
		var err error
		routeStr, err = route.GetPathTemplate()
		if err != nil {
			routeStr, err = route.GetPathRegexp()
			if err != nil {
				routeStr = ""
			}
		}
	}

	opts := []trace.SpanStartOption{
		trace.WithAttributes(httpconv.ServerRequest(tw.service, r)...),
		trace.WithSpanKind(trace.SpanKindServer),
	}

	if tw.publicEndpoint || (tw.publicEndpointFn != nil && tw.publicEndpointFn(r.WithContext(ctx))) {
		opts = append(opts, trace.WithNewRoot())
		// Linking incoming span context if any for public endpoint.
		if s := trace.SpanContextFromContext(ctx); s.IsValid() && s.IsRemote() {
			opts = append(opts, trace.WithLinks(trace.Link{SpanContext: s}))
		}
	}

	if routeStr == "" {
		routeStr = fmt.Sprintf("HTTP %s route not found", r.Method)
	} else {
		rAttr := semconv.HTTPRoute(routeStr)
		opts = append(opts, trace.WithAttributes(rAttr))
	}
	spanName := tw.spanNameFormatter(routeStr, r)
	ctx, span := tw.tracer.Start(ctx, spanName, opts...)
	defer span.End()
	r2 := r.WithContext(ctx)
	rrw := getRRW(w)
	defer putRRW(rrw)

	path := r.URL.Path
	method := r.Method
	appName := util.GetEnv("APP_NAME", "app")
	
	REQUESTS_IN_PROGRESS.WithLabelValues(method,
		path,
		appName).Inc()
	REQUESTS.WithLabelValues(method, 
		path, 
		appName).Inc()
	
	now := time.Now()

	tw.handler.ServeHTTP(rrw.writer, r2)
	if rrw.status > 0 {
		span.SetAttributes(semconv.HTTPStatusCode(rrw.status))
	}
	span.SetStatus(httpconv.ServerStatus(rrw.status))


	REQUESTS_PROCESSING_TIME.WithLabelValues(method,path,appName).Observe(time.Since(now).Seconds())

	sCtx := span.SpanContext()
	traceID := sCtx.TraceID().String()
	spanId := sCtx.SpanID().String()
	REQUESTS_PROCESSING_TIME_EXEMPLAR.(prometheus.ExemplarObserver).ObserveWithExemplar(
					time.Since(now).Seconds(), prometheus.Labels{
						"TraceID": traceID,
						"spanId": spanId,
						"method": method,
						"path": path,
						"appName": appName,
					})
	
	RESPONSES.WithLabelValues(method, 
		path, 
		strconv.Itoa(rrw.status), 
		appName).Inc()
	REQUESTS_IN_PROGRESS.WithLabelValues(method,
		path,
		appName).Dec()
}


type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func NewResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{w, http.StatusOK}
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func ServeHTTPMiddleware(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		method := r.Method
		appName := util.GetEnv("APP_NAME", "app")
		rw := NewResponseWriter(w)
		h.ServeHTTP(rw, r)
		statusCode := rw.statusCode

		switch {
		case statusCode >= 400:
			EXCEPTIONS.WithLabelValues(method, 
				path, 
				http.StatusText(statusCode),
				appName).Inc()
		}
	})
}