package metrics

import (
	"fmt"
	"net"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/klog/v2"
)

var setupErrCount = prometheus.NewCounterVec(prometheus.CounterOpts{
	Namespace: "unbound",
	Subsystem: "nodecache",
	Name:      "setup_errors_total",
	Help:      "The number of errors during periodic network setup for node-cache",
}, []string{"errortype"})

func InitMetrics(ipport string) error {
	if err := serveMetrics(ipport); err != nil {
		return fmt.Errorf("Failed to start metrics handler: %s", err)
	}
	exporter := NewUnboundExporter("/var/run/unbound-control.sock")
	prometheus.MustRegister(exporter)

	registerMetrics()
	klog.V(0).Infof("Started metrics server at %v", ipport)
	return nil
}

func registerMetrics() {
	prometheus.MustRegister(setupErrCount)
	setupErrCount.WithLabelValues("iptables").Add(0)
	setupErrCount.WithLabelValues("iptables_lock").Add(0)
	setupErrCount.WithLabelValues("interface_add").Add(0)
	setupErrCount.WithLabelValues("interface_check").Add(0)
	setupErrCount.WithLabelValues("config").Add(0)
}

func PublishErrorMetric(label string) {
	setupErrCount.WithLabelValues(label).Inc()
}

func serveMetrics(ipport string) error {
	ln, err := net.Listen("tcp", ipport)
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	srv := &http.Server{Handler: mux}
	go func() {
		srv.Serve(ln)
	}()
	return nil
}
