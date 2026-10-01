package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
)

// discardLogger returns a bootstrap.Logger that drops everything, for tests
// that need a real Logger but not its output.
func discardLogger() bootstrap.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestTodo_SVC_013 proves SVC-013's unit-level contract for this
// composition root: the positional up|down|status subcommand is parsed
// correctly, an unrecognized subcommand or a missing database URL fails
// config resolution (ExitConfigError, via bootstrap's own Spec.Validate
// path) before any connection is attempted, migrate serves no health
// endpoint and needs no DatabaseURLField/DBPoolFactory (it opens its own
// *sql.DB), and a malformed connection URL fails fast with no network
// access. TestTodo_SVC_013_Integration (integration_test.go) is the
// real-PostgreSQL complement: up, status and down actually applied and
// journaled against a live server.
func TestTodo_SVC_013(t *testing.T) {
	t.Run("splitCommand", func(t *testing.T) {
		t.Run("empty_args_yields_empty_command", func(t *testing.T) {
			cmd, rest := splitCommand(nil)
			if cmd != "" || rest != nil {
				t.Fatalf("splitCommand(nil) = (%q, %v), want (\"\", nil)", cmd, rest)
			}
		})

		t.Run("first_arg_is_the_command_rest_is_passed_through", func(t *testing.T) {
			cmd, rest := splitCommand([]string{"up", "-database-url=postgres://x"})
			if cmd != "up" {
				t.Fatalf("command = %q, want up", cmd)
			}
			if len(rest) != 1 || rest[0] != "-database-url=postgres://x" {
				t.Fatalf("rest = %v, want [-database-url=postgres://x]", rest)
			}
		})
	})

	t.Run("validateConfig", func(t *testing.T) {
		fields := migrateConfigFields()
		noEnv := func(string) (string, bool) { return "", false }
		withURL := func(t *testing.T) *bootstrap.Values {
			t.Helper()
			v, err := bootstrap.ParseConfig([]string{"-database-url=postgres://x"}, noEnv, fields)
			if err != nil {
				t.Fatalf("ParseConfig: %v", err)
			}
			return v
		}

		t.Run("empty_command_is_a_usage_error", func(t *testing.T) {
			if err := validateConfig("")(withURL(t)); err == nil {
				t.Fatal("validateConfig(\"\") accepted an empty command")
			}
		})

		t.Run("unknown_command_is_rejected", func(t *testing.T) {
			if err := validateConfig("bogus")(withURL(t)); err == nil {
				t.Fatal("validateConfig(\"bogus\") accepted an unknown command")
			}
		})

		for _, cmd := range []string{"up", "down", "status"} {
			t.Run("accepts_"+cmd+"_with_a_database_url", func(t *testing.T) {
				if err := validateConfig(cmd)(withURL(t)); err != nil {
					t.Fatalf("validateConfig(%q): %v", cmd, err)
				}
			})

			t.Run("rejects_"+cmd+"_without_a_database_url", func(t *testing.T) {
				v, err := bootstrap.ParseConfig(nil, noEnv, fields)
				if err != nil {
					t.Fatalf("ParseConfig: %v", err)
				}
				if err := validateConfig(cmd)(v); err == nil {
					t.Fatalf("validateConfig(%q) accepted an empty database-url", cmd)
				}
			})
		}

		t.Run("seed_requires_a_tenant_slug", func(t *testing.T) {
			for _, args := range [][]string{
				{"-database-url=postgres://x"},
			} {
				v, err := bootstrap.ParseConfig(args, noEnv, fields)
				if err != nil {
					t.Fatalf("ParseConfig(%v): %v", args, err)
				}
				if err := validateConfig("seed")(v); err == nil {
					t.Fatalf("validateConfig(seed) accepted %v", args)
				}
			}

			v, err := bootstrap.ParseConfig([]string{
				"-database-url=postgres://x", "-tenant=harborcare",
			}, noEnv, fields)
			if err != nil {
				t.Fatalf("ParseConfig(valid seed): %v", err)
			}
			if err := validateConfig("seed")(v); err != nil {
				t.Fatalf("validateConfig(seed): %v", err)
			}
		})
	})

	t.Run("config_precedence_and_redaction", func(t *testing.T) {
		fields := migrateConfigFields()
		env := map[string]string{EnvDatabaseURL: "postgres://env/db"}
		lookup := func(k string) (string, bool) { v, ok := env[k]; return v, ok }

		v, err := bootstrap.ParseConfig(nil, lookup, fields)
		if err != nil {
			t.Fatalf("ParseConfig: %v", err)
		}
		if got := v.String("database-url"); got != "postgres://env/db" {
			t.Fatalf("database-url = %q, want the env value", got)
		}
		redacted := false
		attrs := v.LogAttrs()
		for i := 0; i+1 < len(attrs); i += 2 {
			if attrs[i] == "database-url" && attrs[i+1] == bootstrap.RedactedValue {
				redacted = true
			}
		}
		if !redacted {
			t.Fatal("database-url was not redacted in LogAttrs")
		}

		v, err = bootstrap.ParseConfig([]string{"-database-url=postgres://flag/db"}, lookup, fields)
		if err != nil {
			t.Fatalf("ParseConfig: %v", err)
		}
		if got := v.String("database-url"); got != "postgres://flag/db" {
			t.Fatalf("database-url = %q, want the flag value (flag beats env)", got)
		}
	})

	t.Run("bootstrap_run_rejects_a_bad_subcommand_before_touching_the_network", func(t *testing.T) {
		s := spec("bogus", []string{"-database-url=postgres://x"})
		s.Getenv = func(string) (string, bool) { return "", false }
		s.Logger = discardLogger()
		var stdout, stderr bytes.Buffer
		s.Stdout = &stdout
		s.Stderr = &stderr

		code := bootstrap.Run(context.Background(), s)
		if code != bootstrap.ExitConfigError {
			t.Fatalf("exit code = %d, want ExitConfigError (%d); stderr=%s", code, bootstrap.ExitConfigError, stderr.String())
		}
	})

	t.Run("bootstrap_run_rejects_a_missing_database_url", func(t *testing.T) {
		s := spec("status", nil)
		s.Getenv = func(string) (string, bool) { return "", false }
		s.Logger = discardLogger()
		s.Stdout = io.Discard
		s.Stderr = io.Discard

		code := bootstrap.Run(context.Background(), s)
		if code != bootstrap.ExitConfigError {
			t.Fatalf("exit code = %d, want ExitConfigError (%d)", code, bootstrap.ExitConfigError)
		}
	})

	t.Run("bootstrap_run_rejects_seed_without_tenant_before_touching_the_network", func(t *testing.T) {
		s := spec("seed", []string{"-database-url=postgres://x"})
		s.Getenv = func(string) (string, bool) { return "", false }
		s.Logger = discardLogger()
		s.Stdout = io.Discard
		s.Stderr = io.Discard

		if code := bootstrap.Run(context.Background(), s); code != bootstrap.ExitConfigError {
			t.Fatalf("exit code = %d, want ExitConfigError (%d)", code, bootstrap.ExitConfigError)
		}
	})

	t.Run("openMigrateDB_fails_fast_on_a_malformed_url_with_no_network", func(t *testing.T) {
		if _, err := openMigrateDB(context.Background(), "not a valid connection url"); err == nil {
			t.Fatal("openMigrateDB accepted a malformed URL")
		}
	})

	t.Run("no_health_endpoint_declared", func(t *testing.T) {
		s := spec("status", []string{"-database-url=postgres://x"})
		if s.HealthAddr != "" {
			t.Fatalf("HealthAddr = %q, want empty: migrate is operator-invoked and short-lived", s.HealthAddr)
		}
	})
}

func TestMigrateCommandFields_AvoidDuplicateApplyFlags(t *testing.T) {
	noEnv := func(string) (string, bool) { return "", false }
	cases := []struct {
		name    string
		command string
		args    []string
		want    bool
	}{
		{name: "status", command: "status", args: []string{"-database-url=postgres://core"}, want: false},
		{name: "document seed upgrade", command: "document seed-upgrade", args: []string{"-database-url=postgres://core", "-document-database-url=postgres://docs", "-tenant=ironridge-demo", "-apply=true"}, want: true},
		{name: "document prune", command: "document prune", args: []string{"-database-url=postgres://core", "-document-database-url=postgres://docs", "-tenant=harborcare-demo", "-owner=hc-050-rafael-torres", "-title-prefix=Bughunt doc ", "-apply=true"}, want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v, err := bootstrap.ParseConfig(tc.args, noEnv, migrateCommandFields(tc.command))
			if err != nil {
				t.Fatalf("ParseConfig(%q): %v", tc.command, err)
			}
			if !v.Has("apply") {
				if tc.want {
					t.Fatal("apply flag was not declared")
				}
				return
			}
			got, err := v.Bool("apply")
			if err != nil {
				t.Fatalf("apply value: %v", err)
			}
			if got != tc.want {
				t.Fatalf("apply = %v, want %v", got, tc.want)
			}
		})
	}
}
