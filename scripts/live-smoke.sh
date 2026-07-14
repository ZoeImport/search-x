#!/usr/bin/env bash
set -euo pipefail

base_url="${SEARCH_BASE_URL:-http://127.0.0.1:8080}"
query="${1:-golang}"

curl --fail-with-body --get "${base_url}/v1/search" \
  --data-urlencode "q=${query}" \
  --data "provider=baidu" \
  --data "limit=10" \
  --data "page=1"
