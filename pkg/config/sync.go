package config

import (
	"crypto/sha256"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"gopkg.in/yaml.v2"
	"k8s.io/klog/v2"
	"k8s.io/utils/clock"
)

type Sync struct {
	configFile    string
	plainFilesDir string
	channel       chan *Config
	latestVersion string
	clock         clock.Clock
	period        time.Duration
}

type syncResult struct {
	Version         string
	AdditionalFiles []string
	ConfigData      []byte
}

func NewSync(configFile string, filesDir string, period time.Duration) *Sync {
	sync := &Sync{
		configFile:    configFile,
		plainFilesDir: filesDir,
		channel:       make(chan *Config),
		period:        period,
		clock:         clock.RealClock{},
	}
	return sync
}

func (s *Sync) Once() (*Config, error) {
	result, err := s.load()
	if err != nil {

		return NewDefaultConfig(), err
	}
	// Always build a template object so we return non-nil
	config, _, err := s.processUpdate(result, true)
	return config, err
}

func (s *Sync) Periodic() <-chan *Config {
	go func() {
		ticker := s.clock.Tick(s.period)
		for {
			if result, err := s.load(); err != nil {
				klog.Errorf("Error loading config from %s: %v", s.configFile, err)
			} else {
				config, changed, err := s.processUpdate(result, false)
				if err == nil && changed {
					s.channel <- config
				}
			}
			<-ticker
		}
	}()
	return s.channel
}

func (s *Sync) load() (syncResult, error) {
	hasher := sha256.New()
	files := make([]string, 0)

	// Load all additional files
	if len(s.plainFilesDir) > 0 {
		err := filepath.Walk(s.plainFilesDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			// special case for the root
			if path == s.plainFilesDir {
				if info.IsDir() {
					return nil
				}
				return fmt.Errorf("config path %q is not a directory", path)
			}

			// don't recurse
			if info.IsDir() {
				return filepath.SkipDir
			}
			// skip hidden files
			filename := filepath.Base(path)
			if strings.HasPrefix(filename, ".") {
				return nil
			}
			filedata, err := ioutil.ReadFile(path)
			if err != nil {
				return err
			}
			if !utf8.Valid(filedata) {
				return fmt.Errorf("non-utf8 data in %s", path)
			}

			// Add data to version hash
			hasher.Write([]byte(filename))
			hasher.Write([]byte{0})
			hasher.Write(filedata)
			hasher.Write([]byte{0})

			// Add files
			files = append(files, filename)

			return nil
		})
		if err != nil {
			return syncResult{}, err
		}
	}

	// Load main configuration file
	configData, err := ioutil.ReadFile(s.configFile)
	if err != nil {
		klog.Warningf("cannot load configuration file %v", err)
	}
	if !utf8.Valid(configData) {
		return syncResult{}, fmt.Errorf("non-utf8 data in %s", s.configFile)
	}

	// Add data to version hash
	hasher.Write([]byte(s.configFile))
	hasher.Write([]byte{0})
	hasher.Write(configData)
	hasher.Write([]byte{0})

	// compute a version string from the hashed data
	version := ""
	if len(configData) > 0 || len(files) > 0 {
		version = fmt.Sprintf("%x", hasher.Sum(nil))
	}
	return syncResult{Version: version, AdditionalFiles: files, ConfigData: configData}, nil
}

func (s *Sync) processUpdate(result syncResult, buildUnchangedConfig bool) (config *Config, changed bool, err error) {
	klog.V(4).Infof("processUpdate %v", result.Version)

	if result.Version != s.latestVersion {
		klog.V(3).Infof("Updating config to version %v (was %v)",
			result.Version, s.latestVersion)
		changed = true
		s.latestVersion = result.Version
	} else {
		klog.V(4).Infof("Config was unchanged (version %v)", s.latestVersion)
		// short-circuit if we haven't been asked to build an unchanged config object
		if !buildUnchangedConfig {
			return
		}
	}

	config = &Config{}

	if result.Version == "" && len(result.ConfigData) == 0 {
		config = NewDefaultConfig()
		config.AdditionalFiles = result.AdditionalFiles
		return
	}

	if err = yaml.Unmarshal([]byte(result.ConfigData), &config); err != nil {
		klog.Warning("Unable to parse configuration. Will continue with default. %v", err)
		config = NewDefaultConfig()
		config.AdditionalFiles = result.AdditionalFiles
		return
	}
	config.AdditionalFiles = result.AdditionalFiles

	return
}
