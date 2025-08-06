package exporter

import "github.com/tinybirdco/kubernetes-event-exporter/pkg/kube"

type Router struct {
	cfg  *Config
	rcvr ReceiverRegistry
}

func (r *Router) ProcessEvent(event *kube.EnhancedEvent) {
	r.cfg.Route.ProcessEvent(event, r.rcvr)
}
