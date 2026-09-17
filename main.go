package main

import (
	"fmt"
	"os"

	"github.com/kwasii1/fakerforge-db-agent/cmd"
	"github.com/kwasii1/fakerforge-db-agent/internal/api"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		usage()
		return 2
	}
	switch args[0] {
	case "login":
		return cmd.RunLogin(args[1:])
	case "logout":
		return cmd.RunLogout(args[1:])
	case "whoami":
		return cmd.RunWhoami(args[1:])
	case "connect":
		return cmd.RunConnect(args[1:])
	case "schema":
		if len(args) >= 2 && args[1] == "pull" {
			return cmd.RunSchemaPull(args[2:])
		}
		fmt.Println("usage: fakerforge schema pull --connection NAME --table TABLE")
		return 2
	case "schemas":
		if len(args) < 2 {
			fmt.Println("usage: fakerforge schemas list|show ...")
			return 2
		}
		switch args[1] {
		case "list":
			return cmd.RunSchemasList(args[2:])
		case "show":
			return cmd.RunSchemasShow(args[2:])
		default:
			fmt.Printf("unknown schemas subcommand %q\n", args[1])
			return 2
		}
	case "datasets":
		fmt.Println("renamed: use `fakerforge schemas list|show` (datasets was renamed to schemas)")
		return 2
	case "push":
		return cmd.RunPush(args[1:])
	case "status":
		return cmd.RunStatus(args[1:])
	case "version", "--version", "-V":
		fmt.Println("fakerforge " + api.Version)
		return 0
	case "help", "--help", "-h":
		usage()
		return 0
	default:
		fmt.Printf("unknown command %q\n", args[0])
		usage()
		return 2
	}
}

func usage() {
	fmt.Printf(`fakerforge %s — pull synthetic data into your local DB

Usage:
  fakerforge login [--api-key KEY]
  fakerforge logout
  fakerforge whoami

  fakerforge connect                         # interactive prompts
  fakerforge connect --name NAME --driver postgres|mysql --host H --port P --database D --user U [--password P]
  fakerforge connect --list
  fakerforge connect --remove NAME
  fakerforge connect --default NAME

  fakerforge schema pull --connection NAME [--tables A,B] [--rows N] [--force] [--regenerate] [--async]

  fakerforge schemas list [--table TABLE] [--format table|json]
  fakerforge schemas show SCHEMA_ID [--table TABLE]

  fakerforge push --schema SCHEMA_ID --connection NAME --table TABLE [--batch-size N] [--dry-run] [--yes]

  fakerforge status
  fakerforge version

Env:
  FAKERFORGE_API_KEY   API key (overrides keychain)
  FAKERFORGE_API_URL   API base URL (default %s)
  FAKERFORGE_DB_PASSWORD  DB password for scripting
  FAKERFORGE_HOME      config dir override (default ~/.fakerforge)
`, api.Version, api.DefaultBaseURL)
}
