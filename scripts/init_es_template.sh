#!/bin/bash
set -e
ES_URL="${ES_URL:-http://localhost:9200}"

curl -s -X PUT "$ES_URL/_index_template/mutong-logs" -H 'Content-Type: application/json' -d '{
  "index_patterns": ["k8s-logs-*", "kafka-logs-*", "redis-logs-*"],
  "priority": 200,
  "template": {
    "mappings": {
      "properties": {
        "@timestamp": { "type": "date" },
        "level": { "type": "keyword" },
        "message": { "type": "text" },
        "kubernetes": {
          "properties": {
            "namespace_name": { "type": "keyword" },
            "pod_name": { "type": "keyword" },
            "container_name": { "type": "keyword" }
          }
        },
        "labels": {
          "properties": {
            "team": { "type": "keyword" },
            "business_unit": { "type": "keyword" }
          }
        }
      }
    }
  }
}'
echo "ES index template mutong-logs created."
