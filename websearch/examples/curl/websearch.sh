#!/usr/bin/env sh
: "${API_KEY:?set API_KEY to an API Market test key}"

curl --fail-with-body 'https://tapi.insmtx.com/v6/se/general/search' \
  --header "Authorization: Bearer ${API_KEY}" \
  --header 'Content-Type: application/json' \
  --data '{"request":{"query":"golang","limit":10,"timeout":"20s","routing":{"providers":["baidu","bing"]}}}'
