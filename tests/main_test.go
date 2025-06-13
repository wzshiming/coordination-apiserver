/*
Copyright 2025 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package tests

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/wzshiming/coordination-apiserver/pkg/cmd"
	genericoptions "k8s.io/apiserver/pkg/server/options"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	clientset *kubernetes.Clientset
)

func TestMain(m *testing.M) {
	opts := &cmd.Options{
		SecureServing: genericoptions.NewSecureServingOptions().WithLoopback(),
	}
	opts.SecureServing.BindPort = 6443

	go func() {
		err := cmd.RunCommand(context.TODO(), opts)
		if err != nil {
			panic(err)
		}
	}()

	for {
		if _, err := os.Stat("apiserver.local.config"); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	config, err := clientcmd.BuildConfigFromFlags("", "kubeconfig.local.yaml")
	if err != nil {
		panic(err)
	}

	clientset, err = kubernetes.NewForConfig(config)
	if err != nil {
		panic(err)
	}

	os.Exit(m.Run())
}
