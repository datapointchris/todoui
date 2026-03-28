package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/datapointchris/todoui/internal/api"
	"github.com/datapointchris/todoui/internal/backend"
	"github.com/datapointchris/todoui/internal/db"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	addr := flag.String("addr", ":8080", "listen address")
	dbPath := flag.String("db", "todoui.db", "path to SQLite database")
	flag.Parse()

	database, err := db.Open(*dbPath)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer func() { _ = database.Close() }()

	b := backend.NewLocalBackend(database)
	srv := api.NewServer(b)

	log.Printf("todoui-server listening on %s (db: %s)", *addr, *dbPath)
	return http.ListenAndServe(*addr, srv)
}
