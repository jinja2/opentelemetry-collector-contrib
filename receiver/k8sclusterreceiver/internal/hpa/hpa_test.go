// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package hpa

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/receiver/receivertest"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/k8sclusterreceiver/internal/metadata"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/k8sclusterreceiver/internal/testutils"
)

func TestHPAMetrics(t *testing.T) {
	hpa := testutils.NewHPA("1")
	ts := pcommon.Timestamp(time.Now().UnixNano())
	mb := metadata.NewMetricsBuilder(metadata.DefaultMetricsBuilderConfig(), receivertest.NewNopSettings(metadata.Type))
	RecordMetrics(mb, hpa, ts)
	m := mb.Emit()

	require.Equal(t, 1, m.ResourceMetrics().Len())
	rm := m.ResourceMetrics().At(0)
	assert.Equal(t,
		map[string]any{
			"k8s.hpa.uid":        "test-hpa-1-uid",
			"k8s.hpa.name":       "test-hpa-1",
			"k8s.namespace.name": "test-namespace",
		},
		rm.Resource().Attributes().AsRaw())

	require.Equal(t, 1, rm.ScopeMetrics().Len())
	sms := rm.ScopeMetrics().At(0)
	require.Equal(t, 4, sms.Metrics().Len())
	sms.Metrics().Sort(func(a, b pmetric.Metric) bool {
		return a.Name() < b.Name()
	})
	testutils.AssertMetricInt(t, sms.Metrics().At(0), "k8s.hpa.current_replicas", pmetric.MetricTypeGauge, 5)
	testutils.AssertMetricInt(t, sms.Metrics().At(1), "k8s.hpa.desired_replicas", pmetric.MetricTypeGauge, 7)
	testutils.AssertMetricInt(t, sms.Metrics().At(2), "k8s.hpa.max_replicas", pmetric.MetricTypeGauge, 10)
	testutils.AssertMetricInt(t, sms.Metrics().At(3), "k8s.hpa.min_replicas", pmetric.MetricTypeGauge, 2)
}

func TestHPAResAttrs(t *testing.T) {
	hpa := testutils.NewHPA("1")

	ts := pcommon.Timestamp(time.Now().UnixNano())

	// Enable additional attributes
	cfg := metadata.DefaultMetricsBuilderConfig()
	cfg.ResourceAttributes.K8sHpaScaletargetrefKind.Enabled = true
	cfg.ResourceAttributes.K8sHpaScaletargetrefName.Enabled = true
	cfg.ResourceAttributes.K8sHpaScaletargetrefApiversion.Enabled = true

	mb := metadata.NewMetricsBuilder(cfg, receivertest.NewNopSettings(metadata.Type))
	RecordMetrics(mb, hpa, ts)
	m := mb.Emit()

	require.Equal(t, 1, m.ResourceMetrics().Len())
	rm := m.ResourceMetrics().At(0)
	assert.Equal(t,
		map[string]any{
			"k8s.hpa.uid":                       "test-hpa-1-uid",
			"k8s.hpa.name":                      "test-hpa-1",
			"k8s.namespace.name":                "test-namespace",
			"k8s.hpa.scaletargetref.kind":       "Deployment",
			"k8s.hpa.scaletargetref.name":       "test-deployment",
			"k8s.hpa.scaletargetref.apiversion": "apps/v1",
		},
		rm.Resource().Attributes().AsRaw())
}

func TestHPACustomMetrics(t *testing.T) {
	hpa := testutils.NewHPA("1")
	ts := pcommon.Timestamp(time.Now().UnixNano())
	settings := receivertest.NewNopSettings(metadata.Type)

	resourceCfg := metadata.DefaultResourceAttributesConfig()
	resourceCfg.K8sHpaScaletargetrefKind.Enabled = true
	resourceCfg.K8sHpaScaletargetrefName.Enabled = true
	resourceCfg.K8sHpaScaletargetrefApiversion.Enabled = true
	rb := metadata.NewResourceBuilder(resourceCfg)

	rm := CustomMetrics(settings, rb, hpa, ts)

	assert.NotNil(t, rm)

	assert.Equal(t,
		map[string]any{
			"k8s.hpa.uid":                       "test-hpa-1-uid",
			"k8s.hpa.name":                      "test-hpa-1",
			"k8s.namespace.name":                "test-namespace",
			"k8s.hpa.scaletargetref.kind":       "Deployment",
			"k8s.hpa.scaletargetref.name":       "test-deployment",
			"k8s.hpa.scaletargetref.apiversion": "apps/v1",
		},
		rm.Resource().Attributes().AsRaw())

	require.Equal(t, 1, rm.ScopeMetrics().Len())
	sm := rm.ScopeMetrics().At(0)
	assert.Equal(t, "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/k8sclusterreceiver",
		sm.Scope().Name())
	assert.Equal(t, settings.BuildInfo.Version, sm.Scope().Version())
	require.Equal(t, 3, sm.Metrics().Len())

	testCases := []struct {
		name          string
		expectedVal   any
		expectedType  string
		expectedAttrs map[string]any
	}{
		{"k8s.hpa.metric.target.cpu.average_utilization", int64(80), "int", map[string]any{"k8s.hpa.metric.type": "Resource"}},
		{"k8s.hpa.metric.target.cpu.average_value", float64(1.5), "double", map[string]any{"k8s.hpa.metric.type": "ContainerResource", "k8s.hpa.metric.container": "test-container-1"}},
		{"k8s.hpa.metric.target.cpu.value", float64(0.5), "double", map[string]any{"k8s.hpa.metric.type": "ContainerResource", "k8s.hpa.metric.container": "test-container-2"}},
	}

	for _, tc := range testCases {
		m := findMetric(t, sm, tc.name)
		assert.Equal(t, pmetric.MetricTypeGauge, m.Type(), "metric type mismatch for %s", tc.name)
		dps := m.Gauge().DataPoints()
		require.Equal(t, 1, dps.Len(), "expected 1 data point for %s", tc.name)
		dp := dps.At(0)

		switch tc.expectedType {
		case "int":
			assert.EqualValues(t, tc.expectedVal, dp.IntValue(), "value mismatch for %s", tc.name)
		case "double":
			assert.EqualValues(t, tc.expectedVal, dp.DoubleValue(), "value mismatch for %s", tc.name)
		}

		for k, v := range tc.expectedAttrs {
			assert.Equal(t, v, dp.Attributes().AsRaw()[k], "attribute %s mismatch for %s", k, tc.name)
		}
	}

	// Test with empty metrics
	emptyHPA := testutils.NewHPA("2")
	emptyHPA.Spec.Metrics = nil
	emptyRM := CustomMetrics(settings, rb, emptyHPA, ts)
	assert.Equal(t, 0, emptyRM.ScopeMetrics().Len(), "Expected empty ResourceMetrics for HPA with no metrics")
}

// Helper to find a metric by name, with debug output if not found
func findMetric(t *testing.T, sms pmetric.ScopeMetrics, name string) pmetric.Metric {
	for i := 0; i < sms.Metrics().Len(); i++ {
		if sms.Metrics().At(i).Name() == name {
			return sms.Metrics().At(i)
		}
	}
	// Print all available metric names for debugging
	var allNames []string
	for i := 0; i < sms.Metrics().Len(); i++ {
		allNames = append(allNames, sms.Metrics().At(i).Name())
	}
	t.Fatalf("metric %s not found. Available metrics: %v", name, allNames)
	return pmetric.Metric{}
}
