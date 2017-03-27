#! /bin/bash

# shellcheck disable=SC1091
. ./config.sh

start_suite "Test short lived connections between containers, with ebpf connection tracking enabled [DISABLED]"

weave_on "$HOST1" launch
scope_on "$HOST1" launch --probe.ebpf.connections=true

## Test disabled: it is currently flaky
## https://github.com/weaveworks/scope/issues/2379

# weave_on "$HOST1" run -d --name nginx nginx
# weave_on "$HOST1" run -d --name client alpine /bin/sh -c "while true; do \
# 	wget http://nginx.weave.local:80/ -O - >/dev/null || true; \
# 	sleep 1; \
# done"
#
# wait_for_containers "$HOST1" 60 nginx client
#
# has_container "$HOST1" nginx
# has_container "$HOST1" client
#
# list_containers "$HOST1"
# list_connections "$HOST1"
#
# has_connection containers "$HOST1" client nginx
#
# endpoints_have_ebpf "$HOST1"

scope_end_suite