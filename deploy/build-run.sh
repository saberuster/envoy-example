#!/usr/bin/env bash

docker build -t envoy-test .
docker run --rm  -p9901:9901 -p10000:10000 envoy-test --service-cluster cluster0 --service-node node0 -c /etc/envoy/envoy.yaml --enable-mutex-tracing

