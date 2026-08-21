// Command dbreset wipes the local database and rebuilds it from the
// migrations in internal/database/migrations, so a broken or messy local
// dev database can be thrown away without reaching for psql by hand.
//
// Run it with:
//
//	go run ./cmd/dbreset
//
// or, to skip the confirmation prompt (e.g. from another script):
//
//	go run ./cmd/dbreset -yes
package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"pco-assistant/internal/database"

	_ "github.com/joho/godotenv/autoload"
)

func main() {
	skipConfirm := flag.Bool("yes", false, "skip the confirmation prompt")
	flag.Parse()

	// Same guardrail as middleware.DevOnly - this is a destructive,
	// effectively irreversible action, so it refuses to run against
	// whatever DB_* env vars happen to be loaded unless APP_ENV explicitly
	// says this is a disposable local database.
	if os.Getenv("APP_ENV") != "local" {
		log.Fatal("dbreset refuses to run unless APP_ENV=local - it deletes every row in every table, and .env should never point that at anything but a throwaway local database")
	}

	if !*skipConfirm && !confirm() {
		fmt.Println("Aborted - nothing was changed.")
		os.Exit(1)
	}

	db, err := database.Open()
	if err != nil {
		log.Fatalf("connect to database: %v", err)
	}
	defer db.Close()

	fmt.Println("Dropping every table and re-running migrations from scratch...")
	if err := database.Reset(db); err != nil {
		log.Fatalf("reset database: %v", err)
	}

	fmt.Println("Database reset complete. Run `go run ./cmd/dbseed` if you want it populated with sample data.")
}

func confirm() bool {
	fmt.Printf("This will permanently delete ALL data in %s@%s:%s/%s and rebuild an empty schema. Type \"reset\" to continue: ",
		os.Getenv("DB_USERNAME"), os.Getenv("DB_HOST"), os.Getenv("DB_PORT"), os.Getenv("DB_DATABASE"))

	answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	return strings.TrimSpace(answer) == "reset"
}
