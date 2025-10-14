package exporter

import (
	"context"
	"sync"
	"time"

	"github.com/tinybirdco/kubernetes-event-exporter/pkg/kube"
	"github.com/tinybirdco/kubernetes-event-exporter/pkg/metrics"
	"github.com/tinybirdco/kubernetes-event-exporter/pkg/sinks"
	"github.com/rs/zerolog/log"
)

const (
	// sinkSendTimeout is the maximum time allowed for a sink to process an event
	sinkSendTimeout = 30 * time.Second
)

// ChannelBasedReceiverRegistry creates two channels for each receiver. One is for receiving events and other one is
// for breaking out of the infinite loop. Each message is passed to receivers
// This might not be the best way to implement such feature. A ring buffer can be better
// and we might need a mechanism to drop the vents
// On closing, the registry sends a signal on all exit channels, and then waits for all to complete.
type ChannelBasedReceiverRegistry struct {
	ch           map[string]chan kube.EnhancedEvent
	exitCh       map[string]chan interface{}
	wg           *sync.WaitGroup
	MetricsStore *metrics.Store
}

func (r *ChannelBasedReceiverRegistry) SendEvent(name string, event *kube.EnhancedEvent) {
	ch := r.ch[name]
	if ch == nil {
		log.Error().Str("name", name).Msg("There is no channel")
		return
	}

	// Use non-blocking send to prevent goroutine buildup
	select {
	case ch <- *event:
		// Event sent successfully
	default:
		// Channel is full, drop event and log warning
		log.Warn().
			Str("sink", name).
			Str("event", event.Message).
			Msg("Channel buffer full, dropping event")
		r.MetricsStore.SendErrors.Inc()
	}
}

func (r *ChannelBasedReceiverRegistry) Register(name string, receiver sinks.Sink) {
	if r.ch == nil {
		r.ch = make(map[string]chan kube.EnhancedEvent)
		r.exitCh = make(map[string]chan interface{})
	}

	// Use buffered channel to prevent blocking during event bursts
	// Buffer size of 1000 allows handling spikes without goroutine buildup
	ch := make(chan kube.EnhancedEvent, 1000)
	exitCh := make(chan interface{})

	r.ch[name] = ch
	r.exitCh[name] = exitCh

	if r.wg == nil {
		r.wg = &sync.WaitGroup{}
	}
	r.wg.Add(1)

	go func() {
		// Ensure panic recovery to prevent sink crashes from stopping event processing
		defer func() {
			if panicErr := recover(); panicErr != nil {
				log.Error().
					Str("sink", name).
					Interface("panic", panicErr).
					Msg("Sink panic recovered, closing sink")
				
				// Protect against Close() panicking again
				func() {
					defer func() {
						if closeErr := recover(); closeErr != nil {
							log.Error().
								Str("sink", name).
								Interface("panic", closeErr).
								Msg("Panic during Close() in recovery handler")
						}
					}()
					receiver.Close()
				}()
				
				log.Info().Str("sink", name).Msg("Closed after panic")
				r.wg.Done()
			}
		}()

	Loop:
		for {
			select {
			case ev := <-ch:
				log.Debug().Str("sink", name).Str("event", ev.Message).Msg("sending event to sink")

				// Create context with timeout to prevent indefinite blocking
				ctx, cancel := context.WithTimeout(context.Background(), sinkSendTimeout)

				// Send event with timeout protection
				err := receiver.Send(ctx, &ev)
				cancel() // Always cancel context to release resources

				if err != nil {
					r.MetricsStore.SendErrors.Inc()
					if err == context.DeadlineExceeded {
						log.Error().
							Err(err).
							Str("sink", name).
							Str("event", ev.Message).
							Dur("timeout", sinkSendTimeout).
							Msg("Sink send timeout exceeded")
					} else {
						log.Debug().
							Err(err).
							Str("sink", name).
							Str("event", ev.Message).
							Msg("Cannot send event")
					}
				}
			case <-exitCh:
				log.Info().Str("sink", name).Msg("Closing the sink")
				break Loop
			}
		}
		receiver.Close()
		log.Info().Str("sink", name).Msg("Closed")
		r.wg.Done()
	}()
}

// Close signals closing to all sinks and waits for them to complete.
// The wait could block indefinitely depending on the sink implementations.
func (r *ChannelBasedReceiverRegistry) Close() {
	// Send exit command and wait for exit of all sinks
	for _, ec := range r.exitCh {
		ec <- 1
	}
	r.wg.Wait()
}
