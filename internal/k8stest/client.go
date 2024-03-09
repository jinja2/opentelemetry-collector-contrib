// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package k8stest

import (
	"errors"
	"fmt"
	"k8s.io/client-go/discovery"
	memory "k8s.io/client-go/discovery/cached"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
)

type K8sClient struct {
	DynamicClient   *dynamic.DynamicClient
	DiscoveryClient *discovery.DiscoveryClient
	Mapper          *restmapper.DeferredDiscoveryRESTMapper
}

func NewK8sClient(kubeconfigPath string) (*K8sClient, error) {

	if kubeconfigPath == "" {
		return nil, errors.New("Please provide file path to load kubeconfig")
	}
	restConfig, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("unable to load kubeconfig from %s: %v", kubeconfigPath, err)
	}

	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("error creating dynamic client: %v", err)
	}

	discoveryClient, err := discovery.NewDiscoveryClientForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("error creating discovery client: %v", err)
	}

	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(discoveryClient))
	return &K8sClient{
		DynamicClient: dynamicClient, DiscoveryClient: discoveryClient, Mapper: mapper}, nil
}
