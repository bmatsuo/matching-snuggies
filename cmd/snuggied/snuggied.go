package main

import (
	"fmt"
	"log"
	"net/http"
)

var config = map[string]string{
	"version": "0.0.0",
}

func snuggiedHandler(config map[string]string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case (r.Method == "GET"):
			fmt.Fprint(w, "a GET")
		case (r.Method == "POST"):
			fmt.Fprint(w, "a POST")
		default:
			http.Error(w, "not supported", http.StatusMethodNotAllowed)
		}
	}
}

func main() {
	http.HandleFunc("/slicer/jobs/", snuggiedHandler(config))

	log.Fatal(http.ListenAndServe(":8888", nil))
}
