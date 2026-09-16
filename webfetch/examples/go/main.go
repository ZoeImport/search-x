package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

func main() {
	body := []byte(`{"url":"https://go.dev/doc/","timeout":"20s","output":{"format":"markdown","max_chars":30000}}`)
	client := &http.Client{Timeout: 25 * time.Second}
	request, err := http.NewRequest(http.MethodPost, "https://tapi.juxonmedia.com/v1/webfetch", bytes.NewReader(body))
	if err != nil {
		panic(err)
	}
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
