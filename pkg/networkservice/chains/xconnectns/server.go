// Copyright (c) 2021 Nordix Foundation.
//
// SPDX-License-Identifier: Apache-2.0
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at:
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package xconnectns provides interpose endpoint implementation for ovs forwarder
// which provides kernel and smartnic endpoints
package xconnectns

import (
	"context"
	"net"
	"net/url"
	"sync"

	"github.com/networkservicemesh/api/pkg/api/networkservice"
	kernelmech "github.com/networkservicemesh/api/pkg/api/networkservice/mechanisms/kernel"
	vxlanmech "github.com/networkservicemesh/api/pkg/api/networkservice/mechanisms/vxlan"
	"github.com/networkservicemesh/sdk-kernel/pkg/kernel/networkservice/connectioncontextkernel"
	"github.com/networkservicemesh/sdk-kernel/pkg/kernel/networkservice/inject"
	"github.com/networkservicemesh/sdk-kernel/pkg/kernel/networkservice/rename"
	"github.com/networkservicemesh/sdk-sriov/pkg/networkservice/common/resourcepool"
	"github.com/networkservicemesh/sdk-sriov/pkg/sriov"
	"github.com/networkservicemesh/sdk-sriov/pkg/sriov/config"
	"github.com/networkservicemesh/sdk/pkg/networkservice/chains/client"
	"github.com/networkservicemesh/sdk/pkg/networkservice/chains/endpoint"
	"github.com/networkservicemesh/sdk/pkg/networkservice/common/clienturl"
	"github.com/networkservicemesh/sdk/pkg/networkservice/common/connect"
	"github.com/networkservicemesh/sdk/pkg/networkservice/common/mechanisms"
	"github.com/networkservicemesh/sdk/pkg/networkservice/common/mechanisms/recvfd"
	"github.com/networkservicemesh/sdk/pkg/networkservice/common/mechanisms/sendfd"
	"github.com/networkservicemesh/sdk/pkg/networkservice/common/mechanismtranslation"
	"github.com/networkservicemesh/sdk/pkg/networkservice/core/chain"
	"github.com/networkservicemesh/sdk/pkg/networkservice/utils/metadata"
	"github.com/networkservicemesh/sdk/pkg/tools/token"
	"google.golang.org/grpc"

	"github.com/networkservicemesh/sdk-ovs/pkg/networkservice/l2ovsconnect"
	"github.com/networkservicemesh/sdk-ovs/pkg/networkservice/mechanisms/kernel"
	"github.com/networkservicemesh/sdk-ovs/pkg/networkservice/mechanisms/vxlan"
	"github.com/networkservicemesh/sdk-ovs/pkg/tools/utils"
)

type ovsConnectNSServer struct {
	endpoint.Endpoint
}

// NewSriovServer - returns sriov implementation of the ovsconnectns network service
func NewSriovServer(ctx context.Context, name string, authzServer networkservice.NetworkServiceServer,
	tokenGenerator token.GeneratorFunc, clientURL *url.URL, bridgeName string, tunnelIPCidr net.IP,
	pciPool resourcepool.PCIPool, resourcePool resourcepool.ResourcePool, sriovConfig *config.Config,
	clientDialOptions ...grpc.DialOption) (endpoint.Endpoint, error) {
	resourceLock := &sync.Mutex{}
	tunnelIP, err := utils.ParseTunnelIP(tunnelIPCidr)
	if err != nil {
		return nil, err
	}
	utils.ConfigureOvS(ctx, bridgeName)
	vxlanInterfacesMutex := &sync.Mutex{}
	vxlanInterfaces := make(map[string]int)
	rv := &ovsConnectNSServer{}
	additionalFunctionality := []networkservice.NetworkServiceServer{
		metadata.NewServer(),
		recvfd.NewServer(),
		// Statically set the url we use to the unix file socket for the NSMgr
		clienturl.NewServer(clientURL),
		connect.NewServer(
			ctx,
			func(ctx context.Context, cc grpc.ClientConnInterface) networkservice.NetworkServiceClient {
				return client.NewClient(ctx,
					cc,
					client.WithName(name),
					client.WithAdditionalFunctionality(
						mechanismtranslation.NewClient(),
						connectioncontextkernel.NewClient(),
						// TODO: uncomment once inject chain element has NewClient
						// inject.NewClient(),
						// mechanisms
						kernel.NewClient(bridgeName),
						// TODO: uncomment once resourcepool chain element has NewClient
						// resourcepool.NewClient(sriov.KernelDriver, resourceLock, pciPool, resourcePool, sriovConfig),
						vxlan.NewClient(tunnelIP, bridgeName, vxlanInterfacesMutex, vxlanInterfaces),
						recvfd.NewClient(),
						sendfd.NewClient(),
					),
				)
			},
			connect.WithDialOptions(clientDialOptions...),
		),
		mechanisms.NewServer(map[string]networkservice.NetworkServiceServer{
			kernelmech.MECHANISM: chain.NewNetworkServiceServer(
				kernel.NewServer(bridgeName),
				resourcepool.NewServer(sriov.KernelDriver, resourceLock, pciPool, resourcePool, sriovConfig),
				rename.NewServer(),
			),
			vxlanmech.MECHANISM: vxlan.NewServer(tunnelIP, bridgeName, vxlanInterfacesMutex, vxlanInterfaces),
		}),
		inject.NewServer(),
		connectioncontextkernel.NewServer(),
		l2ovsconnect.NewServer(bridgeName),
		sendfd.NewServer(),
	}

	rv.Endpoint = endpoint.NewServer(ctx, tokenGenerator,
		endpoint.WithName(name),
		endpoint.WithAuthorizeServer(authzServer),
		endpoint.WithAdditionalFunctionality(additionalFunctionality...))

	return rv, nil
}

// NewKernelServer - returns kernel implementation of the ovsconnectns network service
func NewKernelServer(ctx context.Context, name string, authzServer networkservice.NetworkServiceServer,
	tokenGenerator token.GeneratorFunc, clientURL *url.URL, bridgeName string, tunnelIPCidr net.IP,
	clientDialOptions ...grpc.DialOption) (endpoint.Endpoint, error) {
	tunnelIP, err := utils.ParseTunnelIP(tunnelIPCidr)
	if err != nil {
		return nil, err
	}
	utils.ConfigureOvS(ctx, bridgeName)
	vxlanInterfacesMutex := &sync.Mutex{}
	vxlanInterfaces := make(map[string]int)
	rv := &ovsConnectNSServer{}
	additionalFunctionality := []networkservice.NetworkServiceServer{
		metadata.NewServer(),
		recvfd.NewServer(),
		// Statically set the url we use to the unix file socket for the NSMgr
		clienturl.NewServer(clientURL),
		connect.NewServer(
			ctx,
			func(ctx context.Context, cc grpc.ClientConnInterface) networkservice.NetworkServiceClient {
				return client.NewClient(ctx,
					cc,
					client.WithName(name),
					client.WithAdditionalFunctionality(
						mechanismtranslation.NewClient(),
						connectioncontextkernel.NewClient(),
						// TODO: uncomment once inject chain element has NewClient
						// inject.NewClient(),
						// mechanisms
						kernel.NewClient(bridgeName),
						vxlan.NewClient(tunnelIP, bridgeName, vxlanInterfacesMutex, vxlanInterfaces),
						recvfd.NewClient(),
						sendfd.NewClient(),
					),
				)
			},
			connect.WithDialOptions(clientDialOptions...),
		),
		mechanisms.NewServer(map[string]networkservice.NetworkServiceServer{
			kernelmech.MECHANISM: chain.NewNetworkServiceServer(
				kernel.NewServer(bridgeName),
				rename.NewServer(),
			),
			vxlanmech.MECHANISM: vxlan.NewServer(tunnelIP, bridgeName, vxlanInterfacesMutex, vxlanInterfaces),
		}),
		inject.NewServer(),
		connectioncontextkernel.NewServer(),
		l2ovsconnect.NewServer(bridgeName),
		sendfd.NewServer(),
	}

	rv.Endpoint = endpoint.NewServer(ctx, tokenGenerator,
		endpoint.WithName(name),
		endpoint.WithAuthorizeServer(authzServer),
		endpoint.WithAdditionalFunctionality(additionalFunctionality...))

	return rv, nil
}
