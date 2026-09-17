package cmd

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/kwasii1/fakerforge-db-agent/internal/config"
	"github.com/kwasii1/fakerforge-db-agent/internal/db"
)

// RunConnect implements:
//   fakerforge connect --name N --driver postgres|mysql --host H --port P --database D --user U [--password P]
//   fakerforge connect --list
//   fakerforge connect --remove NAME
//   fakerforge connect --default NAME
func RunConnect(args []string) int {
	fs := flag.NewFlagSet("connect", flag.ContinueOnError)
	name := fs.String("name", "", "Connection name")
	driver := fs.String("driver", "postgres", "Driver: postgres|mysql")
	host := fs.String("host", "localhost", "DB host")
	port := fs.Int("port", 0, "DB port (default 5432 postgres, 3306 mysql)")
	database := fs.String("database", "", "Database name")
	user := fs.String("user", "", "DB user")
	password := fs.String("password", "", "DB password (or FAKERFORGE_DB_PASSWORD env, or prompt)")
	list := fs.Bool("list", false, "List saved connections")
	remove := fs.String("remove", "", "Remove a saved connection")
	def := fs.String("default", "", "Set default connection")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	store, err := config.Default()
	if err != nil {
		fmt.Println(err)
		return 1
	}

	if *list {
		return listConnections(store)
	}
	if *remove != "" {
		if err := store.Remove(*remove); err != nil {
			fmt.Println(err)
			return 1
		}
		if err := config.DeleteConnPassword(*remove); err != nil {
			fmt.Printf("warning: %v\n", err)
		}
		fmt.Printf("Removed connection %q.\n", *remove)
		return 0
	}
	if *def != "" {
		if err := store.SetDefault(*def); err != nil {
			fmt.Println(err)
			return 1
		}
		fmt.Printf("Default connection: %s\n", *def)
		return 0
	}

	if *name == "" || *database == "" || *user == "" {
		fmt.Println("connect requires --name, --database and --user (see --help)")
		return 2
	}
	if *driver != "postgres" && *driver != "mysql" {
		fmt.Println("driver must be postgres|mysql")
		return 2
	}
	p := *port
	if p == 0 {
		p = db.DefaultPort(*driver)
	}
	pw := *password
	if pw == "" {
		pw = os.Getenv("FAKERFORGE_DB_PASSWORD")
	}
	if pw == "" {
		v, err := promptPassword("DB password")
		if err != nil || v == "" {
			fmt.Println("connect cancelled: no password provided")
			return 1
		}
		pw = v
	}

	conn := config.Connection{
		Name: *name, Driver: *driver, Host: *host,
		Port: p, Database: *database, User: *user,
	}

	// Test before saving — never persist broken state.
	d, err := db.Open(conn, pw)
	if err != nil {
		fmt.Printf("connection failed: %v\n", err)
		return 1
	}
	_ = d.Close()

	if err := store.Put(conn); err != nil {
		fmt.Println(err)
		return 1
	}
	if err := config.SetConnPassword(conn.Name, pw); err != nil {
		fmt.Printf("connected OK but failed to save password: %v\n", err)
		return 1
	}
	fmt.Printf("✓ Connected to %s (%s:%d/%s) as %s\n", conn.Name, conn.Host, conn.Port, conn.Database, conn.User)
	return 0
}

func listConnections(store *config.Store) int {
	conns, err := store.List()
	if err != nil {
		fmt.Println(err)
		return 1
	}
	if len(conns) == 0 {
		fmt.Println("No saved connections.")
		return 0
	}
	def, _ := store.GetDefaultName()
	sort.Slice(conns, func(i, j int) bool { return conns[i].Name < conns[j].Name })
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tDRIVER\tHOST\tPORT\tDATABASE\tUSER\tDEFAULT")
	for _, c := range conns {
		mark := ""
		if c.Name == def {
			mark = "*"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\t%s\t%s\n",
			c.Name, c.Driver, c.Host, c.Port, c.Database, c.User, mark)
	}
	_ = w.Flush()
	return 0
}
