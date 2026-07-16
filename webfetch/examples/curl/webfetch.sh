#!/usr/bin/env sh
: "${API_KEY:?set API_KEY to an API Market test key}"

curl --fail-with-body 'https://tapi.insmtx.com/v6/se/general/fetch' \
  --header "Authorization: Bearer ${API_KEY}" \
  --header 'Content-Type: application/json' \
  --data '{"request":{"url":"https://go.dev/doc/","timeout":"20s","output":{"format":"markdown","max_chars":30000}}}'
