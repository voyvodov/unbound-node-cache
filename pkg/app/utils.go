package app

import (
	"strings"

	appmetrics "github.com/hvoyvodov/nodelocaldns/pkg/metrics"
	"k8s.io/kubernetes/pkg/util/iptables"
	"k8s.io/utils/exec"
)

func newIPTables(isIPv6 bool) iptables.Interface {
	execer := exec.New()
	protocol := iptables.ProtocolIPv4
	if isIPv6 {
		protocol = iptables.ProtocolIPv6
	}
	return iptables.New(execer, protocol)
}

func handleIPTablesError(err error) {
	if err == nil {
		return
	}

	if isLockedErr(err) {
		appmetrics.PublishErrorMetric("iptables_lock")
	} else {
		appmetrics.PublishErrorMetric("iptables")
	}
}

func isLockedErr(err error) bool {
	return strings.Contains(err.Error(), "holding the xtables lock")
}
