#!/usr/bin/env sh
curl --fail-with-body 'https://tapi.juxonmedia.com/v1/websearch' \
  --header 'Content-Type: application/json' \
  --data '{"query":"golang","limit":10,"timeout":"20s","routing":{"providers":["brave","duckduckgo"]},"filters":{"include_domains":["go.dev"],"exclude_domains":["example.com"]},"query_options":{"exact_phrases":["context package"],"title_terms":["documentation"],"file_types":["html"]}}'
