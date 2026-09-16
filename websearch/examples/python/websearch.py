import json
import urllib.request

payload = json.dumps(
    {
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
).encode()

request = urllib.request.Request(
    "https://tapi.juxonmedia.com/v1/websearch",
    data=payload,
    headers={"Content-Type": "application/json"},
    method="POST",
)
with urllib.request.urlopen(request, timeout=25) as response:
    print(json.dumps(json.load(response), ensure_ascii=False, indent=2))
