package main

import (
	"github.com/anshulsood11/proglog/log-server/internal/server"
	"log"
)

func main() {
	srv := server.NewHTTPServer(":8080")
	log.Fatal(srv.ListenAndServe())
}
