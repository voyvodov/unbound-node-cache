package healthz

import (
	"encoding/json"
	"net/http"

	"k8s.io/klog/v2"
)

// Checkable Makes sure the object has the Healthz() function
type Checkable interface {
	Healthz() error
}

// Provider is a provder we can check for healthz
type Provider struct {
	Handle Checkable
	Name   string
}

// Instance contains the healthz instance
type Instance struct {
	Providers []Provider
	Detailed  bool
	FailCode  int
}

// Error the structure of the Error object
type Error struct {
	Name    string `json:"name"`
	Message string `json:"message"`
}

// Component struct reprecenting a healthz provider and it's status
type Component struct {
	Name    string `json:"name"`
	Healthy bool   `json:"healthy"`
}

// Response type, we return a json object with {healthy:bool, errors:[]}
type Response struct {
	Services []Component `json:"components,omitempty"`
	Errors   []Error     `json:"errors,omitempty"`
	Healthy  bool        `json:"healthy"`
}

// Healthz returns a http.HandlerFunc for the healthz service
func (h *Instance) Healthz() http.HandlerFunc {

	klog.V(1).Info("[Healthz] health service started")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		var errs []Error
		var srvs []Component

		// Let's check if we have any providers
		// If we don't we should just return 200 OK
		// As long as the web server is running we will assume it's all good
		if h.Providers != nil {
			for _, provider := range h.Providers {
				comp := Component{
					Name:    provider.Name,
					Healthy: true,
				}
				if err := provider.Handle.Healthz(); err != nil {
					errs = append(errs, Error{
						Name:    provider.Name,
						Message: err.Error(),
					})
					comp.Healthy = false
				}
				srvs = append(srvs, comp)
			}
		}

		response := Response{
			Errors:  errs,
			Healthy: true,
		}

		// Detailed will add the services to the JSON Object
		if h.Detailed {
			response.Services = srvs
		}

		if len(errs) > 0 {
			response.Healthy = false
			if h.FailCode != 0 {
				w.WriteHeader(h.FailCode)
			} else {
				w.WriteHeader(http.StatusServiceUnavailable)
			}
		} else {
			w.WriteHeader(http.StatusOK)
		}

		json, err := json.Marshal(response)
		if err != nil {
			klog.Errorf("Unable to marshal healthz errors: %v", err)
		}

		w.Write(json)
	})
}

// Liveness returns a http.HandlerFunc for the liveness probe
func (h *Instance) Liveness() http.HandlerFunc {
	klog.V(1).Info("[Healthz] Liveness service started")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})
}
