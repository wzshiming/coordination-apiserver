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

package apiserver

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apiserver/pkg/registry/rest"
	genericapiserver "k8s.io/apiserver/pkg/server"

	"github.com/wzshiming/coordination-apiserver/pkg/registry"
	coordinationv1 "k8s.io/api/coordination/v1"
)

var (
	Scheme = runtime.NewScheme()
	Codecs = serializer.NewCodecFactory(Scheme)
)

func init() {
	coordinationv1.AddToScheme(Scheme)
	metav1.AddToGroupVersion(Scheme, schema.GroupVersion{Group: "", Version: "v1"})
	Scheme.AddUnversionedTypes(schema.GroupVersion{Group: "", Version: "v1"},
		&metav1.Status{},
		&metav1.APIVersions{},
		&metav1.APIGroupList{},
		&metav1.APIGroup{},
		&metav1.APIResourceList{},
	)
}

type Config struct {
	RecommendedConfig *genericapiserver.RecommendedConfig
}

type CompletedConfig struct {
	CompletedConfig genericapiserver.CompletedConfig
}

func (cfg *Config) Complete() CompletedConfig {
	return CompletedConfig{
		CompletedConfig: cfg.RecommendedConfig.Complete(),
	}
}

type ApiServer struct {
	GenericAPIServer *genericapiserver.GenericAPIServer
}

func (c CompletedConfig) New() (*ApiServer, error) {
	genericServer, err := c.CompletedConfig.New("coordination.k8s.io-apiserver", genericapiserver.NewEmptyDelegate())
	if err != nil {
		return nil, err
	}

	s := &ApiServer{
		GenericAPIServer: genericServer,
	}

	apiGroupInfo := genericapiserver.NewDefaultAPIGroupInfo(coordinationv1.GroupName, Scheme, metav1.ParameterCodec, Codecs)
	restStorage := registry.NewMemoryStore()
	apiGroupInfo.VersionedResourcesStorageMap["v1"] = map[string]rest.Storage{
		"leases": restStorage,
	}

	if err := s.GenericAPIServer.InstallAPIGroup(&apiGroupInfo); err != nil {
		return nil, err
	}

	return s, nil
}
