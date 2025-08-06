package exporter

import (
	"github.com/tinybirdco/kubernetes-event-exporter/pkg/kube"
	"github.com/tinybirdco/kubernetes-event-exporter/pkg/sinks"
)

// ReceiverRegistry registers a receiver with the appropriate sink
type ReceiverRegistry interface {
	SendEvent(string, *kube.EnhancedEvent)
	Register(string, sinks.Sink)
	Close()
}
