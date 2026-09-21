package cmd

import (
	"fmt"
	"strconv"

	"github.com/charmbracelet/huh"
	"github.com/kwasii1/fakerforge-db-agent/internal/config"
	"github.com/kwasii1/fakerforge-db-agent/internal/db"
)

// promptForMissingHuh collects connection fields with a Huh form.
// Flag-provided values pre-fill the form; required fields validate
// non-empty. Falls back to the legacy line prompts if the form cannot
// run (e.g. dumb terminal), so behavior never hard-fails where the old
// prompts worked.
func promptForMissingHuh(name, database, user, driver, host string, port int) (config.Connection, error) {
	defPort := port
	if defPort == 0 {
		defPort = db.DefaultPort(coalesce(driver, "postgres"))
	}
	portStr := ""
	if port != 0 {
		portStr = strconv.Itoa(port)
	}

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Connection name").
				Description("A short label for this database").
				Value(&name).
				Validate(func(s string) error {
					if s == "" {
						return fmt.Errorf("connection name is required")
					}
					return nil
				}),
			huh.NewSelect[string]().
				Title("Driver").
				Options(huh.NewOptions("postgres", "mysql")...).
				Value(&driver),
			huh.NewInput().
				Title("Host").
				Value(&host),
			huh.NewInput().
				Title("Port").
				Description(fmt.Sprintf("Default: %d", defPort)).
				Placeholder(strconv.Itoa(defPort)).
				Value(&portStr).
				Validate(func(s string) error {
					if s == "" {
						return nil
					}
					n, err := strconv.Atoi(s)
					if err != nil || n <= 0 || n > 65535 {
						return fmt.Errorf("port must be 1-65535")
					}
					return nil
				}),
			huh.NewInput().
				Title("Database").
				Value(&database).
				Validate(func(s string) error {
					if s == "" {
						return fmt.Errorf("database is required")
					}
					return nil
				}),
			huh.NewInput().
				Title("User").
				Value(&user).
				Validate(func(s string) error {
					if s == "" {
						return fmt.Errorf("user is required")
					}
					return nil
				}),
		),
	)

	// Pre-fill conventional defaults for empty driver/host.
	if driver == "" {
		driver = "postgres"
	}
	if host == "" {
		host = "localhost"
	}

	if err := form.Run(); err != nil {
		// Huh couldn't run here (e.g. non-tty edge, Ctrl+C):
		// fall back to the legacy prompts.
		return promptForMissing(name, database, user, driver, host, port)
	}

	p := defPort
	if portStr != "" {
		n, err := strconv.Atoi(portStr)
		if err != nil || n <= 0 || n > 65535 {
			return config.Connection{}, fmt.Errorf("port must be 1-65535")
		}
		p = n
	}
	return config.Connection{
		Name: name, Driver: driver, Host: host,
		Port: p, Database: database, User: user,
	}, nil
}

// confirmOverwriteHuh asks whether an existing connection may be replaced.
func confirmOverwriteHuh(name string) (bool, error) {
	var ok bool
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(fmt.Sprintf("Connection %q already exists. Overwrite?", name)).
				Affirmative("Overwrite").
				Negative("Keep existing").
				Value(&ok),
		),
	)
	if err := form.Run(); err != nil {
		return false, err
	}
	return ok, nil
}

// promptPasswordHuh collects the DB password with masked echo.
func promptPasswordHuh() (string, error) {
	var pw string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("DB password").
				EchoMode(huh.EchoModePassword).
				Value(&pw).
				Validate(func(s string) error {
					if s == "" {
						return fmt.Errorf("password is required")
					}
					return nil
				}),
		),
	)
	if err := form.Run(); err != nil {
		return "", err
	}
	return pw, nil
}
