package nanny

import (
	"bufio"
	"bytes"
	"fmt"
	"html/template"
	"io"
	"net"
	"os"
	"os/exec"
	"syscall"

	"github.com/hvoyvodov/nodelocaldns/pkg/config"
	"github.com/hvoyvodov/nodelocaldns/pkg/metrics"
	"github.com/hvoyvodov/nodelocaldns/pkg/util"
	"k8s.io/klog/v2"
)

type RunNannyOpts struct {
	Exec            string
	CheckExec       string
	LocalIPs        []net.IP
	LocalPort       int
	Pid             string
	Template        *template.Template
	RestartOnChange bool
}

type Nanny struct {
	args        []string
	cmd         *exec.Cmd
	ExitChannel chan error
	opts        *RunNannyOpts
}

func NewNanny(opts *RunNannyOpts) *Nanny {
	return &Nanny{
		opts: opts,
	}
}

func (n *Nanny) Configure(c *config.Config) {

	c.Port = 53
	if n.opts.LocalPort > 0 && n.opts.LocalPort < 65535 {
		c.Port = n.opts.LocalPort
	}
	c.Interfaces = n.opts.LocalIPs
	c.Pid = n.opts.Pid

	f, err := os.Create(config.UnboundConfigPath)
	if err != nil {
		klog.Errorf("unable to create Unbound configuration %v", err)
		metrics.PublishErrorMetric("config")
		return
	}
	defer f.Close()

	err = n.opts.Template.Execute(f, c)
	if err != nil {
		klog.Errorf("unable to template Unbound configuration %v", err)
		metrics.PublishErrorMetric("config")
	}
}

func (n *Nanny) Reload() {
	klog.V(2).Infof("Reloading unbound")
	if err := syscall.Kill(n.cmd.Process.Pid, syscall.SIGHUP); err != nil {
		klog.Error("unable to reload unbound %v", err)
	}
}

func (n *Nanny) Start() error {

	if err := n.validate(); err != nil {
		klog.Warningf("configuration cannot be validated %v", err)
		return err
	}

	klog.V(3).Info("configuration is validated")

	n.args = append(n.args, "-d", "-c", config.UnboundConfigPath)

	n.cmd = exec.Command(n.opts.Exec, n.args...)
	stderrReader, err := n.cmd.StderrPipe()
	if err != nil {
		return err
	}

	stdoutReader, err := n.cmd.StdoutPipe()
	if err != nil {
		return err
	}

	if err := n.cmd.Start(); err != nil {
		return err
	}

	logToGlog := func(stream string, reader io.Reader) {
		bufReader := bufio.NewReader(reader)
		for {
			bytes, err := bufReader.ReadBytes('\n')
			if len(bytes) > 0 {
				klog.V(1).Infof("%v", string(bytes))
			}
			if err == io.EOF {
				klog.V(1).Infof("%v", string(bytes))
				klog.Warningf("Got EOF from %v", stream)
				return
			} else if err != nil {
				klog.V(1).Infof("%v", string(bytes))
				klog.Errorf("Error reading from %v: %v", stream, err)
				return
			}
		}
	}

	go logToGlog("stderr", stderrReader)
	go logToGlog("stdout", stdoutReader)

	n.ExitChannel = make(chan error)
	go func() {
		n.ExitChannel <- n.cmd.Wait()
	}()

	return nil
}

func (n *Nanny) validate() error {
	cmd := exec.Command(n.opts.CheckExec, config.UnboundConfigPath)
	klog.V(2).Infof("Validating configuration")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		klog.V(1).Info(stderr.String())
		return err
	}

	return nil
}

func (n *Nanny) Healthz() error {
	if !util.IsFileExists(n.opts.Pid) {
		return fmt.Errorf("pid file for unbound is not found")
	}
	return nil
}
