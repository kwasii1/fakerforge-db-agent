package cmd

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/kwasii1/fakerforge-db-agent/internal/config"
	"github.com/kwasii1/fakerforge-db-agent/internal/db"
	"golang.org/x/term"
)

// RunConnect implements:
//   fakerforge connect                                 # interactive prompts
//   fakerforge connect --name N --driver postgres|mysql --host H --port P --database D --user U [--password P]
//   fakerforge connect --list
//   fakerforge connect --remove NAME
//   fakerforge connect --default NAME
//
// Any of --name/--database/--user omitted on a terminal prompts
// interactively; on a non-terminal stdin the command fails fast.
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

	interactive := *name == "" || *database == "" || *user == ""
	var conn config.Connection
	if interactive {
		// Like login: prompt for what's missing. Never hang in CI/pipes.
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			fmt.Println("connect requires --name, --database and --user when stdin is not a terminal")
			return 2
		}
		var err error
		if conn, err = promptForMissing(*name, *database, *user, *driver, *host, *port); err != nil {
			fmt.Printf("connect cancelled: %v\n", err)
			return 1
		}
		// Don't silently clobber a saved entry + keychain password.
		if _, err := store.Get(conn.Name); err == nil {
			ans, err := promptLine(fmt.Sprintf("Connection %q already exists. Overwrite? [y/N]", conn.Name))
			if err != nil || !isYes(ans) {
				fmt.Println("aborted.")
				return 1
			}
		}
		fmt.Printf("Will connect to %s (%s:%d/%s) as %s\n",
			conn.Name, conn.Host, conn.Port, conn.Database, conn.User)
	} else {
		if *driver != "postgres" && *driver != "mysql" {
			fmt.Println("driver must be postgres|mysql")
			return 2
		}
		p := *port
		if p == 0 {
			p = db.DefaultPort(*driver)
		}
		conn = config.Connection{
			Name: *name, Driver: *driver, Host: *host,
			Port: p, Database: *database, User: *user,
		}
	}
	pw := *password
	if pw == "" {
		pw = os.Getenv("FAKERFORGE_DB_PASSWORD")
	}
	if pw == "" {
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			fmt.Println("connect requires --password or FAKERFORGE_DB_PASSWORD when stdin is not a terminal")
			return 2
		}
		v, err := promptPassword("DB password")
		if err != nil || v == "" {
			fmt.Println("connect cancelled: no password provided")
			return 1
		}
		pw = v
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

// promptForMissing fills a connection interactively. Required fields that are
// empty are prompted until answered; driver/host/port are offered with the
// flag (or conventional) value as default. Driver goes first so the port
// default matches it.
func promptForMissing(name, database, user, driver, host string, port int) (config.Connection, error) {
	var err error
	if name == "" {
		if name, err = promptRequired("Connection name"); err != nil {
			return config.Connection{}, err
		}
	}
	if driver, err = promptDriver(coalesce(driver, "postgres")); err != nil {
		return config.Connection{}, err
	}
	if host, err = promptWithDefault("Host", coalesce(host, "localhost")); err != nil {
		return config.Connection{}, err
	}
	defPort := port
	if defPort == 0 {
		defPort = db.DefaultPort(driver)
	}
	if port, err = promptPort(port, defPort); err != nil {
		return config.Connection{}, err
	}
	if database == "" {
		if database, err = promptRequired("Database"); err != nil {
			return config.Connection{}, err
		}
	}
	if user == "" {
		if user, err = promptRequired("User"); err != nil {
			return config.Connection{}, err
		}
	}
	return config.Connection{
		Name: name, Driver: driver, Host: host,
		Port: port, Database: database, User: user,
	}, nil
}

// promptRequired prompts until a non-empty answer (EOF aborts).
func promptRequired(label string) (string, error) {
	r := bufio.NewReader(os.Stdin)
	for {
		fmt.Printf("%s: ", label)
		s, err := r.ReadString('\n')
		if err != nil {
			return "", fmt.Errorf("no %s provided", strings.ToLower(label))
		}
		if v := strings.TrimSpace(s); v != "" {
			return v, nil
		}
	}
}

// promptWithDefault prompts showing the default; empty input keeps it.
func promptWithDefault(label, def string) (string, error) {
	fmt.Printf("%s [%s]: ", label, def)
	r := bufio.NewReader(os.Stdin)
	s, err := r.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("no %s provided", strings.ToLower(label))
	}
	if v := strings.TrimSpace(s); v != "" {
		return v, nil
	}
	return def, nil
}

// promptDriver loops until postgres|mysql.
func promptDriver(def string) (string, error) {
	for {
		v, err := promptWithDefault("Driver (postgres|mysql)", def)
		if err != nil {
			return "", err
		}
		v = strings.ToLower(v)
		if v == "postgres" || v == "mysql" {
			return v, nil
		}
		fmt.Println("driver must be postgres|mysql")
	}
}

// promptPort keeps an explicitly passed port; otherwise offers the default.
// Non-numeric input re-prompts.
func promptPort(current, def int) (int, error) {
	if current != 0 {
		return current, nil
	}
	for {
		v, err := promptWithDefault("Port", strconv.Itoa(def))
		if err != nil {
			return 0, err
		}
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil || n <= 0 || n > 65535 {
			fmt.Println("port must be 1-65535")
			continue
		}
		return n, nil
	}
}

func coalesce(s, def string) string {
	if s != "" {
		return s
	}
	return def
}

func isYes(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return s == "y" || s == "yes"
}
