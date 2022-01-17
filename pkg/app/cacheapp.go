package app

import (
	"fmt"
	"html/template"
	"os"
	"os/signal"
	"path"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Masterminds/sprig"
	"github.com/hvoyvodov/nodelocaldns/pkg/config"
	"github.com/hvoyvodov/nodelocaldns/pkg/healthz"
	"github.com/hvoyvodov/nodelocaldns/pkg/metrics"
	appmetrics "github.com/hvoyvodov/nodelocaldns/pkg/metrics"
	"github.com/hvoyvodov/nodelocaldns/pkg/nanny"
	"github.com/hvoyvodov/nodelocaldns/pkg/netif"
	"github.com/hvoyvodov/nodelocaldns/pkg/util"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/util/iptables"
	utilnet "k8s.io/utils/net"
)

// ConfigParams lists the configuration options that can be provided to node-cache
type AppParams struct {
	LocalIPStr string // comma separated listen ips for the local cache agent
	// LocalIPs             []net.IP      // parsed ip addresses for the local cache agent to listen for dns requests
	// LocalPort            int           // port to listen for dns requests
	MetricsListenAddress string        // address to serve metrics on
	SetupInterface       bool          // Indicates whether to setup network interface
	InterfaceName        string        // Name of the interface to be created
	Interval             time.Duration // specifies how often to run iptables rules check
	SyncInterval         time.Duration // specifies how often configuration files are checked for changes
	HealthPort           string        // port for the healthcheck
	SetupIptables        bool
	RunNannyOpts         *nanny.RunNannyOpts
	ConfigFile           string
	UnboundTemplatePath  string
}

type iptablesRule struct {
	table iptables.Table
	chain iptables.Chain
	args  []string
}

type CacheApp struct {
	iptables      iptables.Interface
	iptablesRules []iptablesRule
	params        *AppParams
	netifHandle   *netif.NetifManager
	exitChan      chan struct{}
	sigChan       chan os.Signal
	lastError     error
	healthzServer healthz.HealthServer
}

func (c *CacheApp) Init() {
	hinstance := healthz.Instance{
		Detailed: true,
		Providers: []healthz.Provider{
			{
				Handle: c,
				Name:   "cacheapp",
			},
		},
	}

	c.healthzServer = *healthz.NewHealthServer(&hinstance, c.params.HealthPort, c.exitChan)

	if c.params.SetupInterface {
		klog.V(1).Infof("Setup dummy network interface(s) with IPs: %v", c.params.RunNannyOpts.LocalIPs)
		c.netifHandle = netif.NewNetifManager(c.params.RunNannyOpts.LocalIPs)
	}
	if c.params.SetupIptables {
		c.initIptables()
	}
	c.setupNetworking()

	if err := metrics.InitMetrics(c.params.MetricsListenAddress); err != nil {
		c.lastError = err
	}
	c.lastError = nil
}

func (c *CacheApp) initIptables() {
	// using the localIPStr param since we need ip strings here

	for _, localIP := range strings.Split(c.params.LocalIPStr, ",") {
		c.iptablesRules = append(c.iptablesRules, []iptablesRule{
			// Match traffic destined for localIp:localPort and set the flows to be NOTRACKED, this skips connection tracking
			{iptables.Table("raw"), iptables.ChainPrerouting, []string{"-p", "tcp", "-d", localIP,
				"--dport", strconv.Itoa(c.params.RunNannyOpts.LocalPort), "-j", "NOTRACK"}},
			{iptables.Table("raw"), iptables.ChainPrerouting, []string{"-p", "udp", "-d", localIP,
				"--dport", strconv.Itoa(c.params.RunNannyOpts.LocalPort), "-j", "NOTRACK"}},
			// There are rules in filter table to allow tracked connections to be accepted. Since we skipped connection tracking,
			// need these additional filter table rules.
			{iptables.TableFilter, iptables.ChainInput, []string{"-p", "tcp", "-d", localIP,
				"--dport", strconv.Itoa(c.params.RunNannyOpts.LocalPort), "-j", "ACCEPT"}},
			{iptables.TableFilter, iptables.ChainInput, []string{"-p", "udp", "-d", localIP,
				"--dport", strconv.Itoa(c.params.RunNannyOpts.LocalPort), "-j", "ACCEPT"}},
			// Match traffic from localIp:localPort and set the flows to be NOTRACKED, this skips connection tracking
			{iptables.Table("raw"), iptables.ChainOutput, []string{"-p", "tcp", "-s", localIP,
				"--sport", strconv.Itoa(c.params.RunNannyOpts.LocalPort), "-j", "NOTRACK"}},
			{iptables.Table("raw"), iptables.ChainOutput, []string{"-p", "udp", "-s", localIP,
				"--sport", strconv.Itoa(c.params.RunNannyOpts.LocalPort), "-j", "NOTRACK"}},
			// Additional filter table rules for traffic frpm localIp:localPort
			{iptables.TableFilter, iptables.ChainOutput, []string{"-p", "tcp", "-s", localIP,
				"--sport", strconv.Itoa(c.params.RunNannyOpts.LocalPort), "-j", "ACCEPT"}},
			{iptables.TableFilter, iptables.ChainOutput, []string{"-p", "udp", "-s", localIP,
				"--sport", strconv.Itoa(c.params.RunNannyOpts.LocalPort), "-j", "ACCEPT"}},
			// Skip connection tracking for requests to nodelocalDNS that are locally generated, example - by hostNetwork pods
			{iptables.Table("raw"), iptables.ChainOutput, []string{"-p", "tcp", "-d", localIP,
				"--dport", strconv.Itoa(c.params.RunNannyOpts.LocalPort), "-j", "NOTRACK"}},
			{iptables.Table("raw"), iptables.ChainOutput, []string{"-p", "udp", "-d", localIP,
				"--dport", strconv.Itoa(c.params.RunNannyOpts.LocalPort), "-j", "NOTRACK"}},
			// skip connection tracking for healthcheck requests generated by liveness probe to health plugin
			{iptables.Table("raw"), iptables.ChainOutput, []string{"-p", "tcp", "-d", localIP,
				"--dport", c.params.HealthPort, "-j", "NOTRACK"}},
			{iptables.Table("raw"), iptables.ChainOutput, []string{"-p", "tcp", "-s", localIP,
				"--sport", c.params.HealthPort, "-j", "NOTRACK"}},
		}...)
	}
	c.iptables = newIPTables(c.isIPv6())
}

// isIPv6 return if the node-cache is working in IPv6 mode
// LocalIPs are guaranteed to have the same family
func (c *CacheApp) isIPv6() bool {
	if len(c.params.RunNannyOpts.LocalIPs) > 0 {
		return utilnet.IsIPv6(c.params.RunNannyOpts.LocalIPs[0])
	}
	return false
}

func NewCacheApp(params *AppParams) *CacheApp {
	return &CacheApp{
		params:   params,
		sigChan:  make(chan os.Signal, 1),
		exitChan: make(chan struct{}, 1),
	}
}

func (c *CacheApp) Healthz() error {
	return c.lastError
}

func (c *CacheApp) setupNetworking() {
	if c.params.SetupIptables {
		for _, rule := range c.iptablesRules {
			exists, err := c.iptables.EnsureRule(iptables.Prepend, rule.table, rule.chain, rule.args...)
			switch {
			case exists:
				klog.V(4).Infof("iptables rule %v for nodelocaldns already exists", rule)
				continue
			case err == nil:
				klog.Infof("Added back nodelocaldns rule - %v", rule)
				continue
			default:
				// iptables check/rule add failed with error since control reached here.
				klog.Errorf("Error checking/adding iptables rule %v, error - %v", rule, err)
				handleIPTablesError(err)
			}
		}
	}
	if c.params.SetupInterface {
		exists, err := c.netifHandle.EnsureDummyDevice(c.params.InterfaceName)
		if !exists {
			if err != nil {
				klog.Fatalf("Failed to add non-existent interface %s: %s", c.params.InterfaceName, err)
				appmetrics.PublishErrorMetric("interface_add")
			}
			klog.Infof("Added interface - %s", c.params.InterfaceName)
		}
		if err != nil {
			klog.Fatalf("Error checking dummy device %s - %s", c.params.InterfaceName, err)
			appmetrics.PublishErrorMetric("interface_check")
		}
	}
}

func (c *CacheApp) TeardownNetworking() error {
	klog.V(1).Info("Tearing down")
	if c.exitChan != nil {
		c.exitChan <- struct{}{}
	}
	var err error
	if c.params.SetupInterface {
		err = c.netifHandle.RemoveDummyDevice(c.params.InterfaceName)
	}
	if c.params.SetupIptables {
		for _, rule := range c.iptablesRules {
			exists := true
			for exists {
				// check in a loop in case the same rule got added multiple times.
				err = c.iptables.DeleteRule(rule.table, rule.chain, rule.args...)
				if err != nil {
					klog.Errorf("Failed deleting iptables rule %v, error - %v", rule, err)
					handleIPTablesError(err)
				}
				exists, err = c.iptables.EnsureRule(iptables.Prepend, rule.table, rule.chain, rule.args...)
				if err != nil {
					klog.Errorf("Failed checking iptables rule after deletion, rule - %v, error - %v", rule, err)
					handleIPTablesError(err)
				}
			}
			// Delete the rule one last time since EnsureRule creates the rule if it doesn't exist
			err = c.iptables.DeleteRule(rule.table, rule.chain, rule.args...)
		}
	}
	return err
}

func (c *CacheApp) runPeriodic() {
	// if a pidfile is defined in flags, setup iptables as soon as it's created
	if c.params.RunNannyOpts.Pid != "" {
		for {
			if util.IsFileExists(c.params.RunNannyOpts.Pid) {
				break
			}
			klog.V(1).Infof("waiting for unbound pidfile '%s'", c.params.RunNannyOpts.Pid)
			time.Sleep(time.Second * 1)
		}
		// we found the pidfile, coreDNS is running, we can setup networking early
		c.setupNetworking()
	}

	tick := time.NewTicker(c.params.Interval * time.Second)
	for {
		select {
		case <-tick.C:
			c.setupNetworking()
		case <-c.exitChan:
			klog.Warning("Exiting iptables/interface check goroutine")
			return
		}
	}
}

func (c *CacheApp) loadTemplate() error {
	tplName := path.Base(c.params.UnboundTemplatePath)
	tmpl, err := template.New(tplName).Funcs(sprig.FuncMap()).Funcs(template.FuncMap{
		"toYesNo": func(val bool) string {
			if val {
				return "yes"
			}
			return "no"
		},
	}).ParseFiles(c.params.UnboundTemplatePath)
	if err != nil {
		return fmt.Errorf("error getting unbound template: %v", err)
	}
	c.params.RunNannyOpts.Template = tmpl
	return nil
}

func (c *CacheApp) Run() {
	defer klog.Flush()
	defer c.TeardownNetworking()

	nanny := nanny.NewNanny(c.params.RunNannyOpts)

	c.healthzServer.Instance.Providers = append(c.healthzServer.Instance.Providers, healthz.Provider{Handle: nanny, Name: "nanny"})

	// We'll need to handle SIGHUP for reload, and SIGTERM/SIGINT to teardown network
	signal.Notify(c.sigChan, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGINT)

	go c.healthzServer.Start()

	// TODO: Make possible to add additional files here (plain configuration)
	// which will be included in the main unbound configuration
	sync := config.NewSync(c.params.ConfigFile, "", c.params.SyncInterval)

	currentConfig, err := sync.Once()
	if err != nil {
		klog.Errorf("Error getting initial config, using default: %v", err)
		currentConfig = config.NewDefaultConfig()
	}

	if err := c.loadTemplate(); err != nil {
		klog.Error(err)
		return
	}

	// Start periodic check and updates of the IPTables/Interface
	go c.runPeriodic()

	nanny.Configure(currentConfig)
	if err := nanny.Start(); err != nil {
		c.TeardownNetworking()
		klog.Fatalf("Could not start Unbound with initial configuration: %v", err)
	}

	configChan := sync.Periodic()

	for {
		select {
		case sig := <-c.sigChan:
			switch sig {
			case syscall.SIGTERM, syscall.SIGINT:
				klog.V(1).Info("Got SIGTERM. Will exit")
				c.TeardownNetworking()
				os.Exit(0)
			case syscall.SIGHUP:
				klog.V(1).Info("Got SIGHUP. Will reload all configs and templates")
				if err := c.loadTemplate(); err != nil {
					klog.Error(err)
				}
				nanny.Configure(currentConfig)
				nanny.Reload()
			default:
				klog.V(3).Infof("unhandled signal: %v", sig)
			}

		case status := <-nanny.ExitChannel:
			klog.Flush()
			klog.Errorf("unbound exited: %v", status)
			return
		case currentConfig = <-configChan:
			klog.V(0).Infof("reloading unbound with new configuration")
			nanny.Configure(currentConfig)
			nanny.Reload()
		}
	}
}
