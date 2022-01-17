package config

import "net"

const (
	UnboundConfigPath = "/etc/unbound/unbound.conf"
)

type Config struct {
	Cache           ConfigCache  `yaml:"cache"`
	ForwardZones    []ConfigZone `yaml:"forwardZones"`
	StubZones       []ConfigZone `yaml:"stubZones"`
	TCPUpstream     bool         `yaml:"tcpUpstream"`
	RoundRobin      bool         `yaml:"roundRobin"`
	RateLimit       int          `yaml:"rateLimit"`
	NumThreads      int          `yaml:"numThreads"`
	Verbosity       int          `yaml:"verbosity"`
	Port            int
	Logging         ConfigLogging `yaml:"logging"`
	AdditionalFiles []string
	Interfaces      []net.IP
	Pid             string
}

type ConfigLogging struct {
	Queries  bool `yaml:"queries"`
	Replies  bool `yaml:"replies"`
	Servfail bool `yaml:"servfail"`
}

type ConfigCache struct {
	MaxTTL                    int  `yaml:"maxTTL"`
	MinTTL                    int  `yaml:"minTTL"`
	NegativeMaxTTL            int  `yaml:"negativeMaxTTL"`
	Prefetch                  bool `yaml:"prefetch"`
	ServeExpired              bool `yaml:"serveExpired"`
	ServeExpiredTTL           int  `yaml:"serveExpiredTTL"`
	ServeExpiredClientTimeout int  `yaml:"serveExpiredClientTimeout"`
}

type ConfigZone struct {
	Name    string   `yaml:"name"`
	Servers []string `yaml:"servers"`
	UseTCP  bool     `yaml:"useTCP"`
}

func NewDefaultConfig() *Config {
	return &Config{
		Cache:        ConfigCache{},
		ForwardZones: make([]ConfigZone, 0),
		StubZones:    make([]ConfigZone, 0),
		RoundRobin:   false,
		NumThreads:   1,
		RateLimit:    -1,
		Port:         53,
		Interfaces:   make([]net.IP, 0),
	}
}

func (c *Config) Validate() error {

	return nil
}

func (c *Config) validateUpstreamServers() error {

	return nil
}
