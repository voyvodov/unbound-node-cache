package main

import (
	"flag"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/hvoyvodov/nodelocaldns/pkg/app"
	"github.com/hvoyvodov/nodelocaldns/pkg/nanny"
	"k8s.io/klog/v2"
)

var (
	cacheApp app.CacheApp
	version  string
)

func init() {
	params, err := parseAndValidateFlags()
	if err != nil {
		klog.Fatalf("Error parsing flags - %s, Exiting", err)
	}

	cacheApp = *app.NewCacheApp(params)
	cacheApp.Init()
}

func parseAndValidateFlags() (*app.AppParams, error) {
	params := &app.AppParams{
		InterfaceName: "nodelocaldns",
		RunNannyOpts:  &nanny.RunNannyOpts{},
	}

	flag.BoolVar(&params.SetupInterface, "setup-interface", true, "Set to false to skip dummy interface setup")
	flag.StringVar(&params.ConfigFile, "config", "/etc/unbound/unbound.yaml", "Path to Unbound configuration for node-cache")
	flag.DurationVar(&params.SyncInterval, "syncInterval", 10*time.Second, "Interval on which to check for configuration changes")
	flag.DurationVar(&params.Interval, "netSyncInterval", 60*time.Second, "interval(in seconds) to check for iptables rules")
	flag.StringVar(&params.LocalIPStr, "bind-address", "169.254.25.10", "Comma-separated list of IPs to listen on")
	flag.StringVar(&params.MetricsListenAddress, "metrics-listen-address", "0.0.0.0:9253", "address to serve metrics on")
	flag.StringVar(&params.UnboundTemplatePath, "templatePath", "/etc/unbound/unbound.conf.tmpl", "Path to the template Unbound for node-cache")
	flag.StringVar(&params.RunNannyOpts.Pid, "pid-path", "/var/run/unbound.pid", "Path to the pid file to be created")

	flag.BoolVar(&params.SetupIptables, "setup-iptables", true, "indicates whether iptables rules should be setup")
	flag.StringVar(&params.HealthPort, "health-port", "9254", "port used by health plugin")
	// Nanny/Unbound related
	flag.IntVar(&params.RunNannyOpts.LocalPort, "port", 53, "Port on which to listen for DNS requests")
	flag.StringVar(&params.RunNannyOpts.Exec, "unboundExec", "/usr/local/sbin/unbound", "Path to unbound binary")
	flag.StringVar(&params.RunNannyOpts.CheckExec, "unboundCheckConfExec", "/usr/local/sbin/unbound-checkconf", "Path to unbound-checkconf binary")

	klog.InitFlags(nil)
	flag.Parse()

	for _, ipstr := range strings.Split(params.LocalIPStr, ",") {
		newIP := net.ParseIP(ipstr)
		if newIP == nil {
			return params, fmt.Errorf("invalid localip specified - %q", ipstr)
		}

		params.RunNannyOpts.LocalIPs = append(params.RunNannyOpts.LocalIPs, newIP)
	}

	return params, nil
}

func main() {

	cacheApp.Run()
}
