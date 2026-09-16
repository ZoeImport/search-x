import json
import urllib.request

payload = json.dumps(
    {
        "url": "https://go.dev/doc/",
        "timeout": "20s",
        "output": {"format": "markdown", "max_chars": 30000},
    }
).encode()

request = urllib.request.Request(
    "https://tapi.juxonmedia.com/v1/webfetch",
    data=payload,
    headers={"Content-Type": "application/json"},
    method="POST",
)
with urllib.request.urlopen(request, timeout=25) as response:
    print(json.dumps(json.load(response), ensure_ascii=False, indent=2))
