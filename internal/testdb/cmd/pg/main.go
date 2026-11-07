// Command pg runs an embedded PostgreSQL 17 on :5434 for local DB tests
// (Docker is not required). Stop with Ctrl-C.
//
//	go run ./internal/testdb/cmd/pg
//	export TEST_DATABASE_URL=postgres://postgres:postgres@localhost:5434/postgres?sslmode=disable
package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
)

func main() {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	base := filepath.Join(dir, "baoopn-2026", "embedded-pg")
	pg := embeddedpostgres.NewDatabase(embeddedpostgres.DefaultConfig().
		Version(embeddedpostgres.V17).
		Port(5434).
		RuntimePath(filepath.Join(base, "runtime")).
		BinariesPath(filepath.Join(base, "bin")).
		CachePath(filepath.Join(base, "cache")).
		DataPath(filepath.Join(base, "data")))
	if err := pg.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "start embedded postgres:", err)
		os.Exit(1)
	}
	fmt.Println("embedded postgres listening on :5434 (postgres/postgres). Ctrl-C to stop.")
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	if err := pg.Stop(); err != nil {
		fmt.Fprintln(os.Stderr, "stop:", err)
		os.Exit(1)
	}
}
