#!/usr/bin/env sh
: "${API_KEY:?set API_KEY to an API Market test key}"

curl --fail-with-body 'https://tapi.insmtx.com/v6/se/general/search' \
  --header "Authorization: Bearer ${API_KEY}" \
  --header 'Content-Type: application/json' \
  --data '{"request":{"query":"golang","limit":10,"timeout":"20s","routing":{"providers":["brave","duckduckgo"]},"filters":{"include_domains":["go.dev"],"exclude_domains":["example.com"]},"query_options":{"exact_phrases":["context package"],"title_terms":["documentation"],"file_types":["html"]}}}'
