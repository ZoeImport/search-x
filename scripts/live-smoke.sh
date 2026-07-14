#!/usr/bin/env bash
set -euo pipefail

base_url="${SEARCH_BASE_URL:-http://127.0.0.1:8080}"
query="${1:-golang}"

command -v jq >/dev/null 2>&1 || { echo "jq is required for smoke assertions" >&2; exit 1; }

search_json="$(curl --silent --show-error --fail-with-body --get "${base_url}/v1/search" \
  --data-urlencode "q=${query}" \
  --data "provider=auto" \
  --data "limit=10" \
  --data "page=1")"
printf '%s\n' "${search_json}" | jq -e '.provider and (.results | type == "array") and (.results | all(.provider != null))' >/dev/null
printf '%s\n' "${search_json}" | jq '{mode:"get", query, provider, result_count:(.results|length), meta}'

light_json="$(curl --silent --show-error --fail-with-body "${base_url}/v1/search" \
  --header 'Content-Type: application/json' \
  --data "$(jq -n --arg query "${query}" '{query:$query,provider:"auto",limit:5}')")"
printf '%s\n' "${light_json}" | jq -e '.provider and (.results | type == "array")' >/dev/null
printf '%s\n' "${light_json}" | jq '{mode:"post_light", query, provider, result_count:(.results|length), meta}'

content_json="$(curl --silent --show-error --fail-with-body "${base_url}/v1/search" \
  --header 'Content-Type: application/json' \
  --data "$(jq -n --arg query "${query}" '{query:$query,provider:"auto",limit:3,content:{enabled:true,candidate_limit:0,format:"markdown",max_chars:30000}}')")"
printf '%s\n' "${content_json}" | jq -e '
  (.selected_provider != null) and
  (.results | type == "array") and
  (.results | all(.provider != null and .original_rank > 0 and .selected_rank > 0 and (.content | type == "string")))
' >/dev/null
printf '%s\n' "${content_json}" | jq '{mode:"post_content", query, selected_provider, candidate_count, readable_count, meta}'
