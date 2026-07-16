package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

func main() {
	apiKey := os.Getenv("API_KEY")
	if apiKey == "" {
		panic("API_KEY is required")
	}
	body := []byte(`{"request":{"query":"golang","limit":10,"timeout":"20s","routing":{"providers":["brave","duckduckgo"]},"filters":{"include_domains":["go.dev"],"exclude_domains":["example.com"]},"query_options":{"exact_phrases":["context package"],"title_terms":["documentation"],"file_types":["html"]}}}`)
	client := &http.Client{Timeout: 25 * time.Second}
	request, err := http.NewRequest(http.MethodPost, "https://tapi.insmtx.com/v6/se/general/search", bytes.NewReader(body))
	if err != nil {
		panic(err)
	}
	request.Header.Set("Authorization", "Bearer "+apiKey)
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		panic(err)
	}
	defer response.Body.Close()
	var result any
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		panic(err)
	}
	formatted, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(formatted))
}
