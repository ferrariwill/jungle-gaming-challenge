package transport

import (
	"expvar"
	"fmt"
	"net/http"
)

var (
	MetricsWagersProcessed = expvar.NewInt("wagers_processed_total")
	MetricsSQSErrors       = expvar.NewInt("sqs_errors_total")
	MetricsOutboxPublished = expvar.NewInt("outbox_published_total")
	MetricsOutboxErrors    = expvar.NewInt("outbox_errors_total")
)

func handleMetrics(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	fmt.Fprintf(w, "# HELP wagers_processed_total Total wager transactions processed\n")
	fmt.Fprintf(w, "# TYPE wagers_processed_total counter\n")
	fmt.Fprintf(w, "wagers_processed_total %s\n", MetricsWagersProcessed.String())
	fmt.Fprintf(w, "# HELP sqs_errors_total Total SQS processing errors\n")
	fmt.Fprintf(w, "# TYPE sqs_errors_total counter\n")
	fmt.Fprintf(w, "sqs_errors_total %s\n", MetricsSQSErrors.String())
	fmt.Fprintf(w, "# HELP outbox_published_total Total outbox events published\n")
	fmt.Fprintf(w, "# TYPE outbox_published_total counter\n")
	fmt.Fprintf(w, "outbox_published_total %s\n", MetricsOutboxPublished.String())
	fmt.Fprintf(w, "# HELP outbox_errors_total Total outbox publish errors\n")
	fmt.Fprintf(w, "# TYPE outbox_errors_total counter\n")
	fmt.Fprintf(w, "outbox_errors_total %s\n", MetricsOutboxErrors.String())
}
