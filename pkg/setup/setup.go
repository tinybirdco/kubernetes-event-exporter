package setup

import (
	"errors"
	"strings"

	"github.com/tinybirdco/kubernetes-event-exporter/pkg/exporter"
	"github.com/goccy/go-yaml"
)

func ParseConfigFromBytes(configBytes []byte) (exporter.Config, error) {
	var config exporter.Config
	err := yaml.Unmarshal(configBytes, &config)
	if err != nil {
		errMsg := err.Error()
		errLines := strings.Split(errMsg, "\n")
		if len(errLines) > 0 {
			errMsg = errLines[0]
		}
		for _, line := range errLines {
			if strings.Contains(line, "> ") {
				errMsg += ": [ line " + line + "]"
				if strings.Contains(line, "{{") {
					errMsg += ": " + "Need to wrap values with special characters in quotes"
				}
			}
		}
		errMsg = "Cannot parse config to YAML: " + errMsg
		return exporter.Config{}, errors.New(errMsg)
	}

	return config, nil
}
