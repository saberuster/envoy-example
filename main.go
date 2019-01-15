package main

import (
	"fmt"
	envoy_api "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/endpoint"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	als "github.com/envoyproxy/go-control-plane/envoy/config/accesslog/v2"
	alf "github.com/envoyproxy/go-control-plane/envoy/config/filter/accesslog/v2"

	hcm "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
	"github.com/envoyproxy/go-control-plane/pkg/cache"
	xds "github.com/envoyproxy/go-control-plane/pkg/server"
	"github.com/envoyproxy/go-control-plane/pkg/test/resource"

	"github.com/envoyproxy/go-control-plane/pkg/util"
	"google.golang.org/grpc"
	"google.golang.org/grpc/grpclog"
	"io/ioutil"
	"log"
	"net"
	"os"
)

type hash struct {
}

func (h *hash) ID(node *core.Node) string {
	log.Println(node)
	if node.Id == "" {
		return "unknown"
	}
	return node.Id
}

func MakeEndpoint(clusterName string, address string) *envoy_api.ClusterLoadAssignment {
	return &envoy_api.ClusterLoadAssignment{
		ClusterName: clusterName,
		Endpoints: []endpoint.LocalityLbEndpoints{{
			LbEndpoints: []endpoint.LbEndpoint{{
				Endpoint: &endpoint.Endpoint{
					Address: &core.Address{
						Address: &core.Address_SocketAddress{
							SocketAddress: &core.SocketAddress{
								Protocol:      core.TCP,
								Address:       "xxx.xxx.xxxx.xxx",//这里填代理地址
								PortSpecifier: &core.SocketAddress_PortValue{PortValue: 8091},
							},
						},
					},
				},
			}},
		}},
	}
}

func MakeHTTPListener(clusterName, listenerName string, port uint32, route string) *envoy_api.Listener {
	// data source configuration
	rdsSource := core.ConfigSource{}
	rdsSource.ConfigSourceSpecifier = &core.ConfigSource_ApiConfigSource{
		ApiConfigSource: &core.ApiConfigSource{
			ApiType: core.ApiConfigSource_GRPC,
			GrpcServices: []*core.GrpcService{{
				TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
					EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: "xds_cluster"},
				},
			}},
		},
	}

	// access log service configuration
	alsConfig := &als.HttpGrpcAccessLogConfig{
		CommonConfig: &als.CommonGrpcAccessLogConfig{
			LogName: "echo",
			GrpcService: &core.GrpcService{
				TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
					EnvoyGrpc: &core.GrpcService_EnvoyGrpc{
						ClusterName: "xds_cluster",
					},
				},
			},
		},
	}
	alsConfigPbst, err := util.MessageToStruct(alsConfig)
	if err != nil {
		panic(err)
	}

	// HTTP filter configuration
	manager := &hcm.HttpConnectionManager{
		CodecType:  hcm.AUTO,
		StatPrefix: "http",
		RouteSpecifier: &hcm.HttpConnectionManager_Rds{
			Rds: &hcm.Rds{
				ConfigSource:    rdsSource,
				RouteConfigName: route,
			},
		},
		HttpFilters: []*hcm.HttpFilter{{
			Name: util.Router,
		}},
		AccessLog: []*alf.AccessLog{{
			Name: util.HTTPGRPCAccessLog,
			ConfigType: &alf.AccessLog_Config{
				Config: alsConfigPbst,
			},
		}},
	}

	pbst, err := util.MessageToStruct(manager)
	if err != nil {
		panic(err)
	}

	return &envoy_api.Listener{
		Name: listenerName,
		Address: core.Address{
			Address: &core.Address_SocketAddress{
				SocketAddress: &core.SocketAddress{
					Protocol: core.TCP,
					Address:  "0.0.0.0",
					PortSpecifier: &core.SocketAddress_PortValue{
						PortValue: port,
					},
				},
			},
		},
		FilterChains: []listener.FilterChain{{
			Filters: []listener.Filter{{
				Name: util.HTTPConnectionManager,
				ConfigType: &listener.Filter_Config{
					Config: pbst,
				},
			}},
		}},
	}
}

func main() {
	log := grpclog.NewLoggerV2(os.Stdout, ioutil.Discard, ioutil.Discard)
	grpclog.SetLoggerV2(log)
	snapshotCache := cache.NewSnapshotCache(true, &hash{}, log)
	endpoint := MakeEndpoint("cluster0", "")
	cluster := resource.MakeCluster(resource.Xds, "cluster0")
	route := resource.MakeRoute("test_router", "cluster0")
	listener := MakeHTTPListener("cluster0", "listener0", 10000, "test_router")
	snapshot := cache.NewSnapshot("2", []cache.Resource{endpoint},
		[]cache.Resource{cluster},
		[]cache.Resource{route},
		[]cache.Resource{listener})
	err := snapshotCache.SetSnapshot("node0", snapshot)
	if err != nil {
		log.Fatal(err)
	}
	server := xds.NewServer(snapshotCache, nil)
	grpcServer := grpc.NewServer()
	lis, err := net.Listen("tcp", ":8080")
	if err != nil {
		log.Fatal(err)
	}

	discovery.RegisterAggregatedDiscoveryServiceServer(grpcServer, server)
	envoy_api.RegisterEndpointDiscoveryServiceServer(grpcServer, server)
	envoy_api.RegisterClusterDiscoveryServiceServer(grpcServer, server)
	envoy_api.RegisterRouteDiscoveryServiceServer(grpcServer, server)
	envoy_api.RegisterListenerDiscoveryServiceServer(grpcServer, server)
	if err := grpcServer.Serve(lis); err != nil {
		// error handling
		fmt.Println(err)
	}
}
