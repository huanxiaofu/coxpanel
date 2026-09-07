// Command bootstrap-owner provisions the first CoxPanel owner once.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/coxpanel/backend/internal/bootstrap"
	"github.com/coxpanel/backend/internal/db"
)

func main() {
	if err := run(context.Background(), os.Getenv, os.ReadFile, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, getenv func(string) string, readFile func(string) ([]byte, error), stderr io.Writer) error {
	flags := flag.NewFlagSet("bootstrap-owner", flag.ContinueOnError)
	flags.SetOutput(stderr)
	if err := flags.Parse(os.Args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("bootstrap-owner accepts no positional arguments")
	}
	dsn := strings.TrimSpace(getenv("COXPANEL_DB_URL"))
	if dsn == "" {
		return errors.New("COXPANEL_DB_URL is required")
	}
	username := getenv("COXPANEL_BOOTSTRAP_USERNAME")
	email := getenv("COXPANEL_BOOTSTRAP_EMAIL")
	password, err := bootstrapPassword(getenv, readFile)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	database, err := db.NewPool(ctx, dsn)
	if err != nil {
		return errors.New("database connection failed")
	}
	defer database.Close()
	if err := db.Migrate(ctx, database); err != nil {
		return errors.New("database migration failed")
	}
	if err := bootstrap.Run(ctx, database, bootstrap.Options{Username: username, Password: password, Email: email}); err != nil {
		if errors.Is(err, bootstrap.ErrOwnerExists) {
			return errors.New("owner already exists; bootstrap refused")
		}
		if errors.Is(err, bootstrap.ErrInvalidInput) {
			return errors.New("invalid bootstrap username, email, or password")
		}
		return errors.New("owner bootstrap failed")
	}
	fmt.Fprintln(stderr, "owner bootstrap completed")
	return nil
}

func bootstrapPassword(getenv func(string) string, readFile func(string) ([]byte, error)) (string, error) {
	direct := getenv("COXPANEL_BOOTSTRAP_PASSWORD")
	path := strings.TrimSpace(getenv("COXPANEL_BOOTSTRAP_PASSWORD_FILE"))
	if direct != "" && path != "" {
		return "", errors.New("set only one bootstrap password source")
	}
	if path != "" {
		contents, err := readFile(path)
		if err != nil {
			return "", errors.New("bootstrap password file could not be read")
		}
		if len(contents) > 1024 {
			return "", errors.New("bootstrap password file is too large")
		}
		return strings.TrimRight(string(contents), "\r\n"), nil
	}
	if direct == "" {
		return "", errors.New("COXPANEL_BOOTSTRAP_PASSWORD_FILE or COXPANEL_BOOTSTRAP_PASSWORD is required")
	}
	return direct, nil
}
