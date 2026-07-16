import json
import os
import urllib.request

api_key = os.environ["API_KEY"]
payload = json.dumps(
    {
        "request": {
            "query": "golang",
            "limit": 10,
            "timeout": "20s",
            "routing": {"providers": ["brave", "duckduckgo"]},
            "filters": {
                "include_domains": ["go.dev"],
                "exclude_domains": ["example.com"],
            },
            "query_options": {
                "exact_phrases": ["context package"],
                "title_terms": ["documentation"],
                "file_types": ["html"],
            },
        }
    }
).encode()

request = urllib.request.Request(
    "https://tapi.insmtx.com/v6/se/general/search",
    data=payload,
    headers={
        "Authorization": f"Bearer {api_key}",
        "Content-Type": "application/json",
    },
    method="POST",
)
with urllib.request.urlopen(request, timeout=25) as response:
    print(json.dumps(json.load(response), ensure_ascii=False, indent=2))
