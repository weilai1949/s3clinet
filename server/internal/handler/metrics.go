package handler

import (
	"expvar"
	"fmt"
	"net/http"
	"runtime"
	"strconv"
	"sync/atomic"
	"time"
)

var (
	metricHTTPTotal atomic.Int64
	metricHTTP2xx   atomic.Int64
	metricHTTP4xx   atomic.Int64
	metricHTTP5xx   atomic.Int64
	metricStartedAt = time.Now()
)

func init() {
	expvar.Publish("s3c_http_requests_total", expvar.Func(func() any { return metricHTTPTotal.Load() }))
	expvar.Publish("s3c_uptime_seconds", expvar.Func(func() any {
		return int64(time.Since(metricStartedAt).Seconds())
	}))
}

func recordHTTPMetric(status int) {
	metricHTTPTotal.Add(1)
	switch {
	case status >= 500:
		metricHTTP5xx.Add(1)
	case status >= 400:
		metricHTTP4xx.Add(1)
	case status >= 200 && status < 300:
		metricHTTP2xx.Add(1)
	}
}

// metrics 暴露 Prometheus 文本格式指标（无需鉴权，便于内网 scrape）。
func (h *Handler) metrics(w http.ResponseWriter, r *http.Request) {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	fmt.Fprintf(w, "# HELP s3c_http_requests_total Total HTTP requests handled\n")
	fmt.Fprintf(w, "# TYPE s3c_http_requests_total counter\n")
	fmt.Fprintf(w, "s3c_http_requests_total %d\n", metricHTTPTotal.Load())
	fmt.Fprintf(w, "# HELP s3c_http_responses_total HTTP responses by class\n")
	fmt.Fprintf(w, "# TYPE s3c_http_responses_total counter\n")
	fmt.Fprintf(w, "s3c_http_responses_total{class=\"2xx\"} %d\n", metricHTTP2xx.Load())
	fmt.Fprintf(w, "s3c_http_responses_total{class=\"4xx\"} %d\n", metricHTTP4xx.Load())
	fmt.Fprintf(w, "s3c_http_responses_total{class=\"5xx\"} %d\n", metricHTTP5xx.Load())
	fmt.Fprintf(w, "# HELP s3c_uptime_seconds Process uptime in seconds\n")
	fmt.Fprintf(w, "# TYPE s3c_uptime_seconds gauge\n")
	fmt.Fprintf(w, "s3c_uptime_seconds %s\n", strconv.FormatInt(int64(time.Since(metricStartedAt).Seconds()), 10))
	fmt.Fprintf(w, "# HELP s3c_go_goroutines Number of goroutines\n")
	fmt.Fprintf(w, "# TYPE s3c_go_goroutines gauge\n")
	fmt.Fprintf(w, "s3c_go_goroutines %d\n", runtime.NumGoroutine())
	fmt.Fprintf(w, "# HELP s3c_go_memstats_alloc_bytes Bytes allocated and still in use\n")
	fmt.Fprintf(w, "# TYPE s3c_go_memstats_alloc_bytes gauge\n")
	fmt.Fprintf(w, "s3c_go_memstats_alloc_bytes %d\n", ms.Alloc)
	fmt.Fprintf(w, "# HELP s3c_build_info Build version\n")
	fmt.Fprintf(w, "# TYPE s3c_build_info gauge\n")
	fmt.Fprintf(w, "s3c_build_info{version=%q} 1\n", h.version)
}
