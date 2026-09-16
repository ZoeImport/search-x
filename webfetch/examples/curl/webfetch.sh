#!/usr/bin/env sh
curl --fail-with-body 'https://tapi.juxonmedia.com/v1/webfetch' \
  --header 'Content-Type: application/json' \
  --data '{"url":"https://go.dev/doc/","timeout":"20s","output":{"format":"markdown","max_chars":30000}}'
