package cmd

import (
	"flag"
	"fmt"

	"github.com/kwasii1/fakerforge-db-agent/internal/api"
	"github.com/kwasii1/fakerforge-db-agent/internal/config"
	"github.com/kwasii1/fakerforge-db-agent/internal/db"
	"github.com/kwasii1/fakerforge-db-agent/internal/ui"
)

// RunStatus implements: fakerforge status
// Checks API reachable+auth, each saved connection ping, CLI version vs latest.
func RunStatus(args []string) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	apiURL := fs.String("api-url", "", "API base URL")
	apiKey := fs.String("api-key", "", "API key override")
	noColor := fs.Bool("no-color", false, "Disable colored output")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *noColor {
		ui.SetNoColor(true)
	}
	styled := ui.Enabled()
	if styled {
		fmt.Print(ui.Banner())
		fmt.Println()
	}
	failed := false

	key, err := config.ResolveAPIKey(*apiKey)
	if err != nil || key == "" {
		printStatusAuth(styled, false, "not logged in")
		failed = true
	} else {
		base := *apiURL
		if base == "" {
			base = apiURLFromEnv()
		}
		c := api.New(base, key)
		u, err := c.Me()
		if err != nil {
			printStatusAuth(styled, false, err.Error())
			failed = true
		} else {
			printStatusAuth(styled, true, fmt.Sprintf("%s @ %s", u.Email, c.BaseURL))
			if latest, err := c.LatestVersion(); err == nil && latest != "" {
				if latest == api.Version {
					printStatusVersion(styled, "ok", fmt.Sprintf("%s, latest", api.Version))
				} else {
					printStatusVersion(styled, "update", fmt.Sprintf("local %s, latest %s", api.Version, latest))
				}
			} else {
				printStatusVersion(styled, "skip", api.Version)
			}
		}
	}

	store, err := config.Default()
	if err != nil {
		printStatusConnsHeader(styled, false, err.Error())
		return 1
	}
	conns, err := store.List()
	if err != nil {
		printStatusConnsHeader(styled, false, err.Error())
		return 1
	}
	if len(conns) == 0 {
		printStatusConnsHeader(styled, true, "none saved")
	} else {
		printStatusConnsHeader(styled, true, fmt.Sprintf("%d saved", len(conns)))
		def, _ := store.GetDefaultName()
		for _, conn := range conns {
			pw, err := config.GetConnPassword(conn.Name)
			if err != nil {
				printStatusConn(styled, conn.Name, false, fmt.Sprintf("no password: %v", err), conn.Name == def)
				failed = true
				continue
			}
			d, err := db.Open(conn, pw)
			if err != nil {
				printStatusConn(styled, conn.Name, false, err.Error(), conn.Name == def)
				failed = true
				continue
			}
			_ = d.Close()
			printStatusConn(styled, conn.Name, true,
				fmt.Sprintf("%s:%d/%s", conn.Host, conn.Port, conn.Database), conn.Name == def)
		}
	}

	if failed {
		return 1
	}
	return 0
}

// --- styled/printing helpers: plain output stays byte-identical to v1 ---

func printStatusAuth(styled, ok bool, detail string) {
	if !styled {
		if ok {
			fmt.Printf("API auth:   OK (%s)\n", detail)
		} else {
			if detail == "not logged in" {
				fmt.Println("API auth:   FAIL (not logged in)")
			} else {
				fmt.Printf("API auth:   FAIL (%s)\n", detail)
			}
		}
		return
	}
	if ok {
		fmt.Printf("%s %s  %s %s\n", ui.Dot(true),
			ui.Bold("API auth"), ui.Success("OK"), ui.Muted("("+detail+")"))
	} else {
		fmt.Printf("%s %s  %s %s\n", ui.Dot(false),
			ui.Bold("API auth"), ui.Error("FAIL"), ui.Muted("("+detail+")"))
	}
}

func printStatusVersion(styled bool, state, detail string) {
	if !styled {
		switch state {
		case "ok":
			fmt.Printf("CLI version: OK (%s)\n", detail)
		case "update":
			fmt.Printf("CLI version: UPDATE AVAILABLE (%s)\n", detail)
		default:
			fmt.Printf("CLI version: %s (latest check skipped)\n", detail)
		}
		return
	}
	switch state {
	case "ok":
		fmt.Printf("%s %s  %s %s\n", ui.Dot(true),
			ui.Bold("CLI version"), ui.Success("OK"), ui.Muted("("+detail+")"))
	case "update":
		fmt.Printf("%s %s  %s %s\n", ui.Dot(true),
			ui.Bold("CLI version"), ui.Warn("UPDATE AVAILABLE"), ui.Muted("("+detail+")"))
	default:
		fmt.Printf("%s %s  %s\n", ui.Dot(true),
			ui.Bold("CLI version"), ui.Muted(detail+" (latest check skipped)"))
	}
}

func printStatusConnsHeader(styled bool, ok bool, detail string) {
	if !styled {
		if !ok {
			fmt.Printf("connections: FAIL (%s)\n", detail)
			return
		}
		if detail == "none saved" {
			fmt.Println("connections: none saved")
			return
		}
		// Historically no header is printed when connections exist;
		// per-connection lines follow directly. Keep it that way
		// so piped output stays byte-identical.
		return
	}
	if !ok {
		fmt.Printf("%s %s  %s %s\n", ui.Dot(false),
			ui.Section("Connections"), ui.Error("FAIL"), ui.Muted("("+detail+")"))
		return
	}
	if detail == "none saved" {
		fmt.Printf("%s  %s\n", ui.Section("Connections"),
			ui.Muted("none saved — run `fakerforge connect`"))
		return
	}
	fmt.Printf("%s  %s\n", ui.Section("Connections"), ui.Muted("("+detail+")"))
}

func printStatusConn(styled bool, name string, ok bool, detail string, isDefault bool) {
	mark := ""
	if isDefault && ok {
		// Historically only OK lines carry the [default] marker;
		// FAIL lines never had it. Preserve that.
		mark = " [default]"
	}
	if !styled {
		if ok {
			fmt.Printf("  %s: OK (%s)%s\n", name, detail, mark)
		} else {
			fmt.Printf("  %s: FAIL (%s)\n", name, detail)
		}
		return
	}
	defMark := ""
	if isDefault {
		defMark = " " + ui.Muted("[default]")
	}
	if ok {
		fmt.Printf("  %s %s  %s %s%s\n", ui.Dot(true),
			ui.Bold(name), ui.Success("OK"), ui.Muted("("+detail+")"), defMark)
	} else {
		fmt.Printf("  %s %s  %s %s%s\n", ui.Dot(false),
			ui.Bold(name), ui.Error("FAIL"), ui.Muted("("+detail+")"), defMark)
	}
}
