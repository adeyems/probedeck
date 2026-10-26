package main

import (
	"log"
	"net/http"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"state":"ready"}`))
	})
	log.Println("Synthetic example listening on 127.0.0.1:8099")
	log.Fatal(http.ListenAndServe("127.0.0.1:8099", mux))
}
