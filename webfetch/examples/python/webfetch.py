import json
import os
import urllib.request

api_key = os.environ["API_KEY"]
payload = json.dumps(
    {
        "request": {
            "url": "https://go.dev/doc/",
            "timeout": "20s",
            "output": {"format": "markdown", "max_chars": 30000},
        }
    }
).encode()

request = urllib.request.Request(
    "https://tapi.insmtx.com/v6/se/general/fetch",
    data=payload,
    headers={
        "Authorization": f"Bearer {api_key}",
        "Content-Type": "application/json",
    },
    method="POST",
)
with urllib.request.urlopen(request, timeout=25) as response:
    print(json.dumps(json.load(response), ensure_ascii=False, indent=2))
