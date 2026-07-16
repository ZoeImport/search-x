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
	body := []byte(`{"request":{"url":"https://go.dev/doc/","timeout":"20s","output":{"format":"markdown","max_chars":30000}}}`)
	client := &http.Client{Timeout: 25 * time.Second}
	request, err := http.NewRequest(http.MethodPost, "https://tapi.insmtx.com/v6/se/general/fetch", bytes.NewReader(body))
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
