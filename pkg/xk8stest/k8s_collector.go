// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package xk8stest // import "github.com/open-telemetry/opentelemetry-collector-contrib/pkg/xk8stest"

import (
	"bytes"
	"io"
	"maps"
	"os"
	"path/filepath"
	"testing"
	"text/template"
	"time"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
)

// CollectorStartOptions configures the behavior of WaitForCollectorToStart.
type CollectorStartOptions struct {
	// Clientset is an optional typed Kubernetes client. When set, previous container logs
	// are fetched and printed on crash detection, making it easier to debug startup failures.
	Clientset *kubernetes.Clientset
}

func CreateCollectorObjects(t *testing.T, client *K8sClient, testID, manifestsDir string, templateValues map[string]string, host string) []*unstructured.Unstructured {
	return CreateCollectorObjectsWithOptions(t, client, testID, manifestsDir, templateValues, host, CollectorStartOptions{})
}

func CreateCollectorObjectsWithOptions(t *testing.T, client *K8sClient, testID, manifestsDir string, templateValues map[string]string, host string, opts CollectorStartOptions) []*unstructured.Unstructured {
	if manifestsDir == "" {
		manifestsDir = filepath.Join(".", "testdata", "e2e", "collector")
	}
	manifestFiles, err := os.ReadDir(manifestsDir)
	require.NoErrorf(t, err, "failed to read collector manifests directory %s", manifestsDir)
	if host == "" {
		host = HostEndpoint(t)
	}
	var podNamespace string
	var podLabels map[string]any
	createdObjs := make([]*unstructured.Unstructured, 0, len(manifestFiles))
	for _, manifestFile := range manifestFiles {
		tmpl := template.Must(template.New(manifestFile.Name()).ParseFiles(filepath.Join(manifestsDir, manifestFile.Name())))
		manifest := &bytes.Buffer{}
		defaultTemplateValues := map[string]string{
			"Name":         "otelcol-" + testID,
			"HostEndpoint": host,
			"TestID":       testID,
		}
		maps.Copy(defaultTemplateValues, templateValues)
		require.NoError(t, tmpl.Execute(manifest, defaultTemplateValues))
		obj, err := CreateObject(client, manifest.Bytes())
		require.NoErrorf(t, err, "failed to create collector object from manifest %s", manifestFile.Name())
		objKind := obj.GetKind()
		if objKind == "Deployment" || objKind == "DaemonSet" {
			podNamespace = obj.GetNamespace()
			selector := obj.Object["spec"].(map[string]any)["selector"]
			podLabels = selector.(map[string]any)["matchLabels"].(map[string]any)
		}
		createdObjs = append(createdObjs, obj)
	}

	WaitForCollectorToStart(t, client, podNamespace, podLabels, opts)

	return createdObjs
}

func WaitForCollectorToStart(t *testing.T, client *K8sClient, podNamespace string, podLabels map[string]any, opts ...CollectorStartOptions) {
	var startOpts CollectorStartOptions
	if len(opts) > 0 {
		startOpts = opts[0]
	}

	podGVR := schema.GroupVersionResource{Version: "v1", Resource: "pods"}
	listOptions := metav1.ListOptions{LabelSelector: SelectorFromMap(podLabels).String()}
	podTimeoutMinutes := 3
	t.Logf("waiting for collector pods to be ready")
	require.Eventuallyf(t, func() bool {
		list, err := client.DynamicClient.Resource(podGVR).Namespace(podNamespace).List(t.Context(), listOptions)
		require.NoError(t, err, "failed to list collector pods")
		podsNotReady := len(list.Items)
		if podsNotReady == 0 {
			t.Log("did not find collector pods")
			return false
		}

		var pods v1.PodList
		err = runtime.DefaultUnstructuredConverter.FromUnstructured(list.UnstructuredContent(), &pods)
		require.NoError(t, err, "failed to convert unstructured to podList")

		for i := range pods.Items {
			pod := &pods.Items[i]
			podReady := false
			if pod.Status.Phase != v1.PodRunning {
				t.Logf("pod %v is not running, current phase: %v", pod.Name, pod.Status.Phase)
				continue
			}
			for _, cond := range pod.Status.Conditions {
				if cond.Type == v1.PodReady && cond.Status == v1.ConditionTrue {
					podsNotReady--
					podReady = true
				}
			}
			// Add some debug logs for crashing pods
			if !podReady {
				for i := range pod.Status.ContainerStatuses {
					cs := &pod.Status.ContainerStatuses[i]
					restartCount := cs.RestartCount
					if restartCount > 0 && cs.LastTerminationState.Terminated != nil {
						t.Logf("restart count = %d for container %s in pod %s, last terminated reason: %s", restartCount, cs.Name, pod.Name, cs.LastTerminationState.Terminated.Reason)
						t.Logf("termination message: %s", cs.LastTerminationState.Terminated.Message)
						// Fetch previous container logs if a clientset was provided
						if startOpts.Clientset != nil {
							fetchPreviousContainerLogs(t, startOpts.Clientset, podNamespace, pod.Name, cs.Name)
						}
					}
				}
			}
		}
		if podsNotReady == 0 {
			t.Logf("collector pods are ready")
			return true
		}
		return false
	}, time.Duration(podTimeoutMinutes)*time.Minute, 2*time.Second,
		"collector pods were not ready within %d minutes", podTimeoutMinutes)
}

func fetchPreviousContainerLogs(t *testing.T, clientset *kubernetes.Clientset, namespace, podName, containerName string) {
	tailLines := int64(50)
	logOpts := &v1.PodLogOptions{Container: containerName, Previous: true, TailLines: &tailLines}
	req := clientset.CoreV1().Pods(namespace).GetLogs(podName, logOpts)
	logStream, err := req.Stream(t.Context())
	if err != nil {
		t.Logf("failed to fetch previous logs for %s/%s: %v", podName, containerName, err)
		return
	}
	defer logStream.Close()
	logBytes, err := io.ReadAll(logStream)
	if err != nil {
		t.Logf("failed to read previous logs for %s/%s: %v", podName, containerName, err)
		return
	}
	if len(logBytes) > 0 {
		t.Logf("previous container logs for %s/%s:\n%s", podName, containerName, string(logBytes))
	}
}
