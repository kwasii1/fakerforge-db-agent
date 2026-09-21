package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/kwasii1/fakerforge-db-agent/cmd"
	"github.com/kwasii1/fakerforge-db-agent/internal/api"
	"github.com/kwasii1/fakerforge-db-agent/internal/ui"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	// Global --no-color: strip before dispatch so stdlib FlagSets
	// in subcommands don't choke on it.
	args = stripNoColor(args)
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
	case "schema", "schemas":
		if len(args) < 2 {
			fmt.Println("usage: fakerforge schema pull --connection NAME [--tables A,B] | fakerforge schemas list|show ...")
			return 2
		}
		switch args[1] {
		case "pull":
			return cmd.RunSchemaPull(args[2:])
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
	if ui.Enabled() {
		usageStyled()
		return
	}
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

  fakerforge schema pull --connection NAME [--tables A,B] [--rows N] [--force] [--regenerate] [--async] [--no-progress]

  fakerforge schemas list [--table TABLE] [--format table|json]
  fakerforge schemas show SCHEMA_ID [--table TABLE]

  fakerforge push --schema SCHEMA_ID --connection NAME [--table TABLE] [--batch-size N] [--dry-run] [--yes] [--append]

  fakerforge status
  fakerforge version

Env:
  FAKERFORGE_API_KEY   API key (overrides keychain)
  FAKERFORGE_API_URL   API base URL (default %s)
  FAKERFORGE_DB_PASSWORD  DB password for scripting
  FAKERFORGE_HOME      config dir override (default ~/.fakerforge)
`, api.Version, api.DefaultBaseURL)
}

// stripNoColor extracts a global --no-color flag anywhere in args.
// Returns the remaining args.
func stripNoColor(args []string) []string {
	rest := make([]string, 0, len(args))
	for _, a := range args {
		if a == "--no-color" {
			ui.SetNoColor(true)
			continue
		}
		if strings.HasPrefix(a, "--no-color=") {
			continue
		}
		rest = append(rest, a)
	}
	return rest
}

// usageStyled renders the same help with Lip Gloss theme (TTY only).
func usageStyled() {
	var b strings.Builder
	b.WriteString(ui.Banner())
	b.WriteString("\n")
	b.WriteString("  " + ui.Bold("Usage:") + " " + ui.Muted("fakerforge <command> [flags]") + "\n")

	section := func(title string, rows [][2]string) {
		b.WriteString("\n  " + ui.Section(title) + "\n")
		w := 0
		for _, r := range rows {
			if len(r[0]) > w {
				w = len(r[0])
			}
		}
		for _, r := range rows {
			cmd := r[0] + strings.Repeat(" ", w-len(r[0]))
			b.WriteString("    " + ui.Title(cmd))
			if r[1] != "" {
				b.WriteString("  " + ui.Muted(r[1]))
			}
			b.WriteString("\n")
		}
	}

	section("Account", [][2]string{
		{"login", "authenticate with an API key"},
		{"logout", "remove the stored key"},
		{"whoami", "show the current user"},
	})
	section("Databases", [][2]string{
		{"connect", "save a database connection (interactive)"},
		{"connect --list", "list saved connections"},
		{"connect --remove NAME", "delete a connection"},
		{"connect --default NAME", "set the default connection"},
	})
	section("Schemas", [][2]string{
		{"schema pull --connection NAME", "introspect + generate synthetic data"},
		{"schemas list", "list your schemas"},
		{"schemas show SCHEMA_ID", "show a schema or table"},
	})
	section("Data", [][2]string{
		{"push --schema ID --connection NAME", "stream generated rows into your DB"},
	})
	section("System", [][2]string{
		{"status", "check auth, connections and version"},
		{"version", "print the CLI version"},
	})

	b.WriteString("\n  " + ui.Section("Environment") + "\n")
	b.WriteString("    " + ui.Title("FAKERFORGE_API_KEY") + "      " + ui.Muted("API key (overrides keychain)") + "\n")
	b.WriteString("    " + ui.Title("FAKERFORGE_API_URL") + "      " + ui.Muted("API base URL (default "+api.DefaultBaseURL+")") + "\n")
	b.WriteString("    " + ui.Title("FAKERFORGE_DB_PASSWORD") + "  " + ui.Muted("DB password for scripting") + "\n")
	b.WriteString("    " + ui.Title("FAKERFORGE_HOME") + "       " + ui.Muted("config dir override (default ~/.fakerforge)") + "\n")

	fmt.Println(b.String())
}
