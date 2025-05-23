package telemetry

import (
	"time"

	"github.com/hashicorp/go-metrics"
)

// Common metric key constants
const (
	MetricKeyPreBlocker   = "pre_blocker"
	MetricKeyBeginBlocker = "begin_blocker"
	MetricKeyEndBlocker   = "end_blocker"
	MetricLabelNameModule = "module"
)

// NewLabel creates a new instance of Label with name and value
func NewLabel(name, value string) metrics.Label {
	return metrics.Label{Name: name, Value: value}
}

// ModuleMeasureSince provides a short hand method for emitting a time measure
// metric for a module with a given set of keys. If any global labels are defined,
// they will be added to the module label.
func ModuleMeasureSince(module string, start time.Time, keys ...string) {
	if !IsTelemetryEnabled() {
		return
	}

	metrics.MeasureSinceWithLabels(
		keys,
		start.UTC(),
		append([]metrics.Label{NewLabel(MetricLabelNameModule, module)}, globalLabels...),
	)
}

// ModuleSetGauge provides a short hand method for emitting a gauge metric for a
// module with a given set of keys. If any global labels are defined, they will
// be added to the module label.
func ModuleSetGauge(module string, val float32, keys ...string) {
	if !IsTelemetryEnabled() {
		return
	}

	metrics.SetGaugeWithLabels(
		keys,
		val,
		append([]metrics.Label{NewLabel(MetricLabelNameModule, module)}, globalLabels...),
	)
}

// IncrCounter provides a wrapper functionality for emitting a counter metric with
// global labels (if any).
func IncrCounter(val float32, keys ...string) {
	if !IsTelemetryEnabled() {
		return
	}

	metrics.IncrCounterWithLabels(keys, val, globalLabels)
}

// IncrCounterWithLabels provides a wrapper functionality for emitting a counter
// metric with global labels (if any) along with the provided labels.
func IncrCounterWithLabels(keys []string, val float32, labels []metrics.Label) {
	if !IsTelemetryEnabled() {
		return
	}

	metrics.IncrCounterWithLabels(keys, val, append(labels, globalLabels...))
}

// SetGauge provides a wrapper functionality for emitting a gauge metric with
// global labels (if any).
func SetGauge(val float32, keys ...string) {
	if !IsTelemetryEnabled() {
		return
	}

	metrics.SetGaugeWithLabels(keys, val, globalLabels)
}

// SetGaugeWithLabels provides a wrapper functionality for emitting a gauge
// metric with global labels (if any) along with the provided labels.
func SetGaugeWithLabels(keys []string, val float32, labels []metrics.Label) {
	if !IsTelemetryEnabled() {
		return
	}

	metrics.SetGaugeWithLabels(keys, val, append(labels, globalLabels...))
}

// MeasureSince provides a wrapper functionality for emitting a time measure
// metric with global labels (if any).
func MeasureSince(start time.Time, keys ...string) {
	if !IsTelemetryEnabled() {
		return
	}

	metrics.MeasureSinceWithLabels(keys, start.UTC(), globalLabels)
}

// Now return the current time if telemetry is enabled or a zero time if it's not
func Now() time.Time {
	if !IsTelemetryEnabled() {
		return time.Time{}
	}

	return time.Now()
}
