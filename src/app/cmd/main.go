package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/sirupsen/logrus"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"otel-golang-observability/pkg/monitoring"
	"otel-golang-observability/pkg/util"
)

type HealthStatus struct {
	Status string `json:"status"`
}

func main() {

	// Setup OpenTelemetry Tracing
	tracingShutdown := monitoring.InitTracer()
	defer tracingShutdown()

	appName := util.GetEnv("APP_NAME", "app")

	router := mux.NewRouter()
	router.Use(monitoring.RouteMiddleware(appName))

	router.Path("/metrics").Handler(promhttp.HandlerFor(
		prometheus.DefaultGatherer,
		promhttp.HandlerOpts{
			EnableOpenMetrics: true,
		},
	)).Methods("GET")

	router.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		status := HealthStatus{
			Status: "OK",
		}
		w.Header().Set("Content-Type", "application/json")
		err := json.NewEncoder(w).Encode(status)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}).Methods("GET")

	router.HandleFunc("/", func (w http.ResponseWriter, r *http.Request)  {
		ctx := r.Context()
		log := monitoring.NewLogrus(ctx).WithFields(logrus.Fields{
			"path": "/",
		})
		log.Info("Hello World")

		fmt.Fprintf(w, "Hello World")
	}).Methods("GET")

	router.HandleFunc("/io_task", func (w http.ResponseWriter, r *http.Request)  {
		time.Sleep(time.Second * 2)
		fmt.Fprintf(w, "IO bound task finish!")

		ctx := r.Context()
		log := monitoring.NewLogrus(ctx).WithFields(logrus.Fields{
			"path": "/io_task",
		})
		log.Error("io task")
	}).Methods("GET")

	router.HandleFunc("/cpu_task", func (w http.ResponseWriter, r *http.Request)  {
		cpu_sum := 0
		nums := make([]int, 10000)
		for i := range nums {
			cpu_sum = cpu_sum + i
			//time.Sleep(time.Second * 1)
		}
		fmt.Fprintf(w, "CPU bound task finish!")

		ctx := r.Context()
		log := monitoring.NewLogrus(ctx).WithFields(logrus.Fields{
			"path": "/cpu_task",
		})
		log.Info("cpu task")

	}).Methods("GET")

	router.HandleFunc("/random_status", func (w http.ResponseWriter, r *http.Request)  {
		status := HealthStatus{
			Status: "random_status",
		}
		w.Header().Set("Content-Type", "application/json")
		err := json.NewEncoder(w).Encode(status)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

		ctx := r.Context()
		log := monitoring.NewLogrus(ctx).WithFields(logrus.Fields{
			"path": "/random_status",
		})
		log.Error("random status")
	}).Methods("GET")

	router.HandleFunc("/random_sleep", func (w http.ResponseWriter, r *http.Request)  {
		rand.Seed(time.Now().UnixNano())
		n := rand.Intn(10)
		time.Sleep(time.Duration(n)*time.Second)
		fmt.Fprintf(w, "random sleep")

		ctx := r.Context()
		log := monitoring.NewLogrus(ctx).WithFields(logrus.Fields{
			"path": "/random_sleep",
		})
		log.Info("random sleep")

	}).Methods("GET")

	router.HandleFunc("/error_test", func (w http.ResponseWriter, r *http.Request)  {
		http.Error(w, "got error!!!!", http.StatusInternalServerError)
	}).Methods("GET")

	router.HandleFunc("/chain", func (w http.ResponseWriter, r *http.Request)  {
		ctx := r.Context()
		header := r.Header.Clone()

		// A slice of sample websites
		urls := []string{
			"http://localhost:8080/",
			util.GetEnv("TARGET_ONE_HOST", "app-b"),
			util.GetEnv("TARGET_TWO_HOST", "app-c"),
		}
		var wg sync.WaitGroup
		for _, u := range urls {
			// Increment the wait group counter
			wg.Add(1)
			go func(ctx context.Context, url string, hader http.Header) {
				// Decrement the counter when the go routine completes
				defer wg.Done()
				// Call the function check
				httpGet(ctx, url, header)
			}(ctx, u, header)
		}
		// Wait for all the checkWebsite calls to finish
		wg.Wait()

		log := monitoring.NewLogrus(ctx).WithFields(logrus.Fields{
			"path": "/chain",
		})
		log.Info("Chain Finished")
		fmt.Fprintf(w, "chain")

	}).Methods("GET")


	address := "0.0.0.0:8080"

	srv := &http.Server{
		Addr:              address,
		Handler: 		   monitoring.ServeHTTPMiddleware(router),
		ReadTimeout:       1 * time.Second,
		ReadHeaderTimeout: 1 * time.Second,
		WriteTimeout:      1 * time.Second,
		IdleTimeout:       1 * time.Second,
	}

	fmt.Println("Starting server", address)
	log.Fatal(srv.ListenAndServe())
}

func httpGet(ctx context.Context, url string, hader http.Header) error {
	client := http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)}
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header = hader
	if err != nil {
		return err
	}
	_, err = client.Do(req)
	if err != nil {
		return err
	}
	return nil
}