package main

import (
	"fmt"
	"net/http"
	"strings"
)

func main() {
	article := strings.Repeat("This browser-rendered article contains meaningful content for resource measurement. ", 30)
	http.HandleFunc("/js", func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(response, `<!doctype html><html><head><title>Loading</title></head><body><div id="app"></div><script src="/app.js"></script></body></html>`)
	})
	http.HandleFunc("/app.js", func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/javascript")
		fmt.Fprintf(response, `setTimeout(()=>{document.title='Rendered Article';document.querySelector('#app').innerHTML='<article><h1>Rendered Article</h1><p>%s</p></article>'},50);`, article)
	})
	if err := http.ListenAndServe("127.0.0.1:18084", nil); err != nil {
		panic(err)
	}
}
