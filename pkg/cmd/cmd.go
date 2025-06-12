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

package cmd

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"

	"github.com/spf13/cobra"
	openapinamer "k8s.io/apiserver/pkg/endpoints/openapi"
	"k8s.io/apiserver/pkg/server"
	genericoptions "k8s.io/apiserver/pkg/server/options"
	utilcompatibility "k8s.io/apiserver/pkg/util/compatibility"
	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/klog/v2"

	"github.com/wzshiming/coordination-apiserver/pkg/apiserver"
	"github.com/wzshiming/coordination-apiserver/pkg/openapi"
)

type Options struct {
	SecureServing *genericoptions.SecureServingOptionsWithLoopback
}

func (o *Options) Flags() cliflag.NamedFlagSets {
	fs := cliflag.NamedFlagSets{}
	o.SecureServing.AddFlags(fs.FlagSet("secure serving"))
	return fs
}

func (o *Options) Complete() error {
	return nil
}

func (o *Options) ServerConfig() (*apiserver.Config, error) {
	apiservercfg, err := o.apiserverConfig()
	if err != nil {
		return nil, err
	}

	return &apiserver.Config{
		RecommendedConfig: apiservercfg,
	}, nil
}

func (o *Options) apiserverConfig() (*server.RecommendedConfig, error) {
	if err := o.SecureServing.MaybeDefaultWithSelfSignedCerts("localhost", nil, []net.IP{net.ParseIP("127.0.0.1")}); err != nil {
		return nil, fmt.Errorf("error creating self-signed certificates: %v", err)
	}

	serverConfig := server.NewRecommendedConfig(apiserver.Codecs)
	if err := o.SecureServing.ApplyTo(&serverConfig.SecureServing, &serverConfig.LoopbackClientConfig); err != nil {
		return nil, err
	}

	namer := openapinamer.NewDefinitionNamer(apiserver.Scheme)
	serverConfig.OpenAPIConfig = server.DefaultOpenAPIConfig(openapi.GetOpenAPIDefinitions, namer)
	serverConfig.OpenAPIConfig.Info.Title = "coordination-apiserver"
	serverConfig.OpenAPIConfig.Info.Version = "0.0.1"

	serverConfig.OpenAPIV3Config = server.DefaultOpenAPIV3Config(openapi.GetOpenAPIDefinitions, namer)
	serverConfig.OpenAPIV3Config.Info.Title = "coordination-apiserver"
	serverConfig.OpenAPIV3Config.Info.Version = "0.0.1"

	serverConfig.EffectiveVersion = utilcompatibility.DefaultBuildEffectiveVersion()
	return serverConfig, nil
}

func NewServerCommand(ctx context.Context) *cobra.Command {
	opts := &Options{
		SecureServing: genericoptions.NewSecureServingOptions().WithLoopback(),
	}

	cmd := &cobra.Command{
		Short: "Launch coordination-apiserver",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Complete(); err != nil {
				return err
			}
			return runCommand(ctx, opts)
		},
	}

	fs := cmd.Flags()
	nfs := opts.Flags()
	for _, f := range nfs.FlagSets {
		fs.AddFlagSet(f)
	}

	local := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	klog.InitFlags(local)
	nfs.FlagSet("logging").AddGoFlagSet(local)

	return cmd
}

func runCommand(ctx context.Context, opts *Options) error {
	serverCfg, err := opts.ServerConfig()
	if err != nil {
		return err
	}

	server, err := serverCfg.Complete().New()
	if err != nil {
		return err
	}

	return server.GenericAPIServer.PrepareRun().RunWithContext(ctx)
}
