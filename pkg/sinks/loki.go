package sinks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/tinybirdco/kubernetes-event-exporter/pkg/kube"
	"github.com/rs/zerolog/log"
)

type promtailStream struct {
	Stream map[string]string `json:"stream"`
	Values [][]string        `json:"values"`
}

type LokiMsg struct {
	Streams []promtailStream `json:"streams"`
}

type LokiConfig struct {
	Layout       map[string]interface{} `yaml:"layout"`
	StreamLabels map[string]string      `yaml:"streamLabels"`
	TLS          TLS                    `yaml:"tls"`
	URL          string                 `yaml:"url"`
	Headers      map[string]string      `yaml:"headers"`
	Username     string                 `yaml:"username"`
	Password     string                 `yaml:"password"`
}

type Loki struct {
	cfg       *LokiConfig
	transport *http.Transport
}

func NewLoki(cfg *LokiConfig) (Sink, error) {
	tlsClientConfig, err := setupTLS(&cfg.TLS)
	if err != nil {
		return nil, fmt.Errorf("failed to setup TLS: %w", err)
	}
	return &Loki{cfg: cfg, transport: &http.Transport{
		Proxy:           http.ProxyFromEnvironment,
		TLSClientConfig: tlsClientConfig,
	}}, nil
}

func generateTimestamp() string {
	return strconv.FormatInt(time.Now().Unix(), 10) + "000000000"
}

// containsTemplatePattern checks if a string contains Go template syntax like {{ .Field }}
func containsTemplatePattern(s string) bool {
	return strings.Contains(s, "{{") && strings.Contains(s, "}}")
}

func (l *Loki) Send(ctx context.Context, ev *kube.EnhancedEvent) error {
	eventBody, err := serializeEventWithLayout(l.cfg.Layout, ev)
	if err != nil {
		return err
	}
	timestamp := generateTimestamp()
	
	// Process stream labels, applying templates only to values that contain template syntax
	processedLabels := make(map[string]string)
	for k, v := range l.cfg.StreamLabels {
		// Check if the value contains template syntax
		if containsTemplatePattern(v) {
			processed, err := GetString(ev, v)
			if err != nil {
				log.Debug().Err(err).Msgf("parse template for stream label failed: %s", v)
				processedLabels[k] = v
			} else {
				log.Debug().Msgf("stream label: {%s: %s}", k, processed)
				processedLabels[k] = processed
			}
		} else {
			// Use the original value for non-template strings
			processedLabels[k] = v
		}
	}
	
	a := LokiMsg{
		Streams: []promtailStream{{
			Stream: processedLabels,
			Values: [][]string{{timestamp, string(eventBody)}},
		}},
	}
	reqBody, err := json.Marshal(a)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, l.cfg.URL, bytes.NewBuffer(reqBody))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")

	// Set basic auth if username and password are provided
	if l.cfg.Username != "" && l.cfg.Password != "" {
		req.SetBasicAuth(l.cfg.Username, l.cfg.Password)
	}

	for k, v := range l.cfg.Headers {
		realValue, err := GetString(ev, v)
		if err != nil {
			log.Debug().Err(err).Msgf("parse template failed: %s", v)
			req.Header.Add(k, v)
		} else {
			log.Debug().Msgf("request header: {%s: %s}", k, realValue)
			req.Header.Add(k, realValue)
		}
	}

	client := http.DefaultClient
	resp, err := client.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if !(resp.StatusCode >= 200 && resp.StatusCode < 300) {
		return errors.New("not successfull (2xx) response: " + string(body))
	}

	return nil
}

func (l *Loki) Close() {
	l.transport.CloseIdleConnections()
}
