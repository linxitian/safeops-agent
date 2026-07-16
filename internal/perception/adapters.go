package perception

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
)

// Adapter is deliberately transport-neutral. Deployments can hand the bounded
// payload to a Prometheus remote-write or OpenTelemetry exporter without
// making the core Agent depend on either telemetry platform.
type Adapter interface {
	Name() string
	Adapt(context.Context, CollectionBatch) (any, error)
}

type PrometheusAdapterConfig struct {
	Namespace       string            `json:"namespace"`
	ConstantLabels  map[string]string `json:"constant_labels,omitempty"`
	IncludeSeverity bool              `json:"include_severity"`
	MaxSamples      int               `json:"max_samples"`
}

type PrometheusSample struct {
	Name      string            `json:"name"`
	Value     float64           `json:"value"`
	Timestamp time.Time         `json:"timestamp"`
	Labels    map[string]string `json:"labels"`
}

type PrometheusPayload struct {
	Samples           []PrometheusSample `json:"samples"`
	SkippedNonNumeric int                `json:"skipped_non_numeric"`
	Truncated         bool               `json:"truncated"`
}

type PrometheusAdapter struct{ Config PrometheusAdapterConfig }

func (PrometheusAdapter) Name() string { return "prometheus" }

func (a PrometheusAdapter) Adapt(ctx context.Context, batch CollectionBatch) (any, error) {
	namespace := strings.TrimSpace(a.Config.Namespace)
	if namespace == "" {
		namespace = "safeops"
	}
	if !prometheusNamePattern.MatchString(namespace) {
		return nil, errors.New("prometheus namespace must match [a-zA-Z_:][a-zA-Z0-9_:]*")
	}
	max := a.Config.MaxSamples
	if max <= 0 {
		max = 5000
	}
	if max > 10000 {
		return nil, errors.New("prometheus max_samples must not exceed 10000")
	}
	payload := PrometheusPayload{}
	for _, value := range batch.Observations {
		if err := ctx.Err(); err != nil {
			return payload, err
		}
		number, ok := numericValue(value.Value)
		if !ok || math.IsInf(number, 0) || math.IsNaN(number) {
			payload.SkippedNonNumeric++
			continue
		}
		if len(payload.Samples) >= max {
			payload.Truncated = true
			break
		}
		labels := copyLabels(a.Config.ConstantLabels, 64)
		for key, label := range value.Labels {
			if len(labels) >= 64 {
				break
			}
			labels[sanitizeLabelName(key)] = boundedLabel(label)
		}
		labels["host"] = boundedLabel(value.Host)
		labels["resource_type"] = boundedLabel(value.ResourceType)
		labels["resource_id"] = boundedLabel(value.ResourceID)
		labels["source"] = boundedLabel(value.Source)
		labels["unit"] = boundedLabel(value.Unit)
		if a.Config.IncludeSeverity {
			labels["severity"] = boundedLabel(value.Severity)
		}
		payload.Samples = append(payload.Samples, PrometheusSample{Name: namespace + "_" + sanitizeMetricName(value.MetricName), Value: number, Timestamp: value.Timestamp, Labels: labels})
	}
	return payload, nil
}

type OpenTelemetryAdapterConfig struct {
	ServiceName        string            `json:"service_name"`
	ResourceAttributes map[string]string `json:"resource_attributes,omitempty"`
	MaxDataPoints      int               `json:"max_data_points"`
}

type OpenTelemetryMetric struct {
	Name       string            `json:"name"`
	Value      float64           `json:"value"`
	Unit       string            `json:"unit"`
	Timestamp  time.Time         `json:"timestamp"`
	Attributes map[string]string `json:"attributes"`
}

type OpenTelemetryLog struct {
	Body       string            `json:"body"`
	Severity   string            `json:"severity"`
	Timestamp  time.Time         `json:"timestamp"`
	Attributes map[string]string `json:"attributes"`
}

type OpenTelemetryPayload struct {
	ServiceName string                `json:"service_name"`
	Metrics     []OpenTelemetryMetric `json:"metrics"`
	Logs        []OpenTelemetryLog    `json:"logs"`
	Truncated   bool                  `json:"truncated"`
}

type OpenTelemetryAdapter struct{ Config OpenTelemetryAdapterConfig }

func (OpenTelemetryAdapter) Name() string { return "opentelemetry" }

func (a OpenTelemetryAdapter) Adapt(ctx context.Context, batch CollectionBatch) (any, error) {
	service := strings.TrimSpace(a.Config.ServiceName)
	if service == "" {
		service = "safeops-agent"
	}
	if len(service) > 128 {
		return nil, errors.New("opentelemetry service_name must not exceed 128 bytes")
	}
	max := a.Config.MaxDataPoints
	if max <= 0 {
		max = 5000
	}
	if max > 10000 {
		return nil, errors.New("opentelemetry max_data_points must not exceed 10000")
	}
	payload := OpenTelemetryPayload{ServiceName: service}
	for _, value := range batch.Observations {
		if err := ctx.Err(); err != nil {
			return payload, err
		}
		if len(payload.Metrics)+len(payload.Logs) >= max {
			payload.Truncated = true
			break
		}
		attributes := copyLabels(a.Config.ResourceAttributes, 64)
		for key, label := range value.Labels {
			if len(attributes) >= 64 {
				break
			}
			attributes[key] = boundedLabel(label)
		}
		attributes["safeops.observation_id"] = value.ObservationID
		attributes["safeops.source"] = value.Source
		attributes["host.name"] = boundedLabel(value.Host)
		attributes["safeops.resource.type"] = value.ResourceType
		attributes["safeops.resource.id"] = boundedLabel(value.ResourceID)
		if number, ok := numericValue(value.Value); ok && !math.IsInf(number, 0) && !math.IsNaN(number) {
			payload.Metrics = append(payload.Metrics, OpenTelemetryMetric{Name: "safeops." + strings.ReplaceAll(sanitizeMetricName(value.MetricName), "_", "."), Value: number, Unit: value.Unit, Timestamp: value.Timestamp, Attributes: attributes})
			continue
		}
		body := fmt.Sprint(value.Value)
		if len(body) > 4096 {
			body = body[:4096] + "…"
		}
		attributes["safeops.metric.name"] = value.MetricName
		attributes["safeops.unit"] = value.Unit
		payload.Logs = append(payload.Logs, OpenTelemetryLog{Body: body, Severity: value.Severity, Timestamp: value.Timestamp, Attributes: attributes})
	}
	return payload, nil
}

var prometheusNamePattern = regexp.MustCompile(`^[a-zA-Z_:][a-zA-Z0-9_:]*$`)

func sanitizeMetricName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	var out strings.Builder
	for index, char := range value {
		valid := char == '_' || char == ':' || char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || index > 0 && char >= '0' && char <= '9'
		if valid {
			out.WriteRune(char)
		} else {
			out.WriteByte('_')
		}
	}
	return out.String()
}

func sanitizeLabelName(value string) string {
	value = sanitizeMetricName(value)
	if strings.HasPrefix(value, "__") {
		return "safeops_" + strings.TrimLeft(value, "_")
	}
	return value
}

func boundedLabel(value string) string {
	if len(value) > 512 {
		return value[:512] + "…"
	}
	return value
}

func copyLabels(values map[string]string, limit int) map[string]string {
	out := map[string]string{}
	for key, value := range values {
		if len(out) >= limit {
			break
		}
		out[sanitizeLabelName(key)] = boundedLabel(value)
	}
	return out
}

func numericValue(value any) (float64, bool) {
	switch value := value.(type) {
	case int:
		return float64(value), true
	case int8:
		return float64(value), true
	case int16:
		return float64(value), true
	case int32:
		return float64(value), true
	case int64:
		return float64(value), true
	case uint:
		return float64(value), true
	case uint8:
		return float64(value), true
	case uint16:
		return float64(value), true
	case uint32:
		return float64(value), true
	case uint64:
		return float64(value), true
	case float32:
		return float64(value), true
	case float64:
		return value, true
	default:
		return 0, false
	}
}
