#!/bin/sh
while true; do
  k6 run /tests/test.js --out experimental-prometheus-rw=http://groundcover-custom-metrics.groundcover.svc.cluster.local
  sleep 1
done
