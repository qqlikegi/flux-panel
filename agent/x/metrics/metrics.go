package metrics

import (
	"sync/atomic"

	"github.com/qqlikegi/forwardz/agent/x/lib/core/metrics"
)

const (
	// Number of services. Labels: host.
	MetricServicesGauge metrics.MetricName = "forwardz_services"
	// Total service requests. Labels: host, service.
	MetricServiceRequestsCounter metrics.MetricName = "forwardz_service_requests_total"
	// Number of in-flight requests. Labels: host, service.
	MetricServiceRequestsInFlightGauge metrics.MetricName = "forwardz_service_requests_in_flight"
	// Request duration historgram. Labels: host, service.
	MetricServiceRequestsDurationObserver metrics.MetricName = "forwardz_service_request_duration_seconds"
	// Total service input data transfer size in bytes. Labels: host, service.
	MetricServiceTransferInputBytesCounter metrics.MetricName = "forwardz_service_transfer_input_bytes_total"
	// Total service output data transfer size in bytes. Labels: host, service.
	MetricServiceTransferOutputBytesCounter metrics.MetricName = "forwardz_service_transfer_output_bytes_total"
	// Chain node connect duration histogram. Labels: host, chain, node.
	MetricNodeConnectDurationObserver metrics.MetricName = "forwardz_chain_node_connect_duration_seconds"
	// Total service handler errors. Labels: host, service.
	MetricServiceHandlerErrorsCounter metrics.MetricName = "forwardz_service_handler_errors_total"
	// Total chain connect errors. Labels: host, chain, node.
	MetricChainErrorsCounter metrics.MetricName = "forwardz_chain_errors_total"
)

var (
	defaultMetrics metrics.Metrics = NewMetrics()
	enabled atomic.Bool
)

func Enable(b bool) {
	enabled.Store(b)
}

func IsEnabled() bool {
	return enabled.Load()
}

func GetCounter(name metrics.MetricName, labels metrics.Labels) metrics.Counter {
	if IsEnabled() {
		return defaultMetrics.Counter(name, labels)
	}
	return noop.Counter(name, labels)
}

func GetGauge(name metrics.MetricName, labels metrics.Labels) metrics.Gauge {
	if IsEnabled() {
		return defaultMetrics.Gauge(name, labels)
	}
	return noop.Gauge(name, labels)
}

func GetObserver(name metrics.MetricName, labels metrics.Labels) metrics.Observer {
	if IsEnabled() {
		return defaultMetrics.Observer(name, labels)
	}
	return noop.Observer(name, labels)
}
