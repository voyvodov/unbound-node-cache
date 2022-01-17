package healthz

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"k8s.io/klog/v2"
)

type HealthServer struct {
	ListenPort string
	Instance   *Instance
	exitChan   chan struct{}
	doneChan   chan bool
}

func NewHealthServer(instance *Instance, listenPort string, exitChan chan struct{}) *HealthServer {
	return &HealthServer{
		ListenPort: listenPort,
		Instance:   instance,
		doneChan:   make(chan bool),
		exitChan:   exitChan,
	}
}

func (h *HealthServer) Handle() (*http.Server, error) {
	if h.Instance == nil {
		return nil, errors.New("no healthz configuration passed to the server")
	}
	mux := http.NewServeMux()

	// Add the webserver to the list of healthz providers?
	mux.Handle("/healthz", h.Instance.Healthz())
	mux.Handle("/liveness", h.Instance.Liveness())

	server := &http.Server{
		Addr:         fmt.Sprintf(":%s", h.ListenPort),
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  15 * time.Second,
	}

	return server, nil
}

func (h *HealthServer) Start() (chan bool, error) {
	server, err := h.Handle()
	if err != nil {
		close(h.doneChan)
		return h.doneChan, err
	}

	go h.gracefulShutdown(server)
	klog.V(0).Infof("[Healthz-Server]: listening on :%s", h.ListenPort)

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		klog.Errorf("[Healthz-Server]: Could not listen on ", h.ListenPort, err)
	}
	return h.doneChan, nil
}

func (h *HealthServer) gracefulShutdown(server *http.Server) {
	<-h.exitChan
	klog.V(0).Info("[Healthz-Server]: shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	server.SetKeepAlivesEnabled(false)
	if err := server.Shutdown(ctx); err != nil {
		klog.Errorf("[Healthz-Server]: Could not gracefully shutdown the server: %v\n", err)
	}
	close(h.doneChan)
	klog.V(1).Info("[Healthz-Server]: Shut down")
}
