// Command migrate is a thin composition root built on
// internal/platform/bootstrap.Run: it resolves this role's typed
// configuration and hands bootstrap one one-shot Workload (migrate.go) that
// applies the embedded SQL-first migration tree with Goose over pgx and
// journals every step per DB-006: artifact digest, tool version, checksum,
// owner, start and finish, and the resulting status.
//
//	migrate up      apply every pending migration
//	migrate down    roll back the most recently applied migration
//	migrate status  report the schema version, digest and per-migration state
//	migrate chat up apply the independent chat database's migration set
//	migrate chat status report the chat schema version and per-migration state
//	migrate document up apply the independent document database's migration set
//	migrate document status report the document schema version
//	migrate document seed load the HarborCare demo document library (-tenant
//	defaults to harborcare-demo; personas come from -database-url)
//	migrate document embed enqueue meaning-search index jobs for -tenant with
//	the model named by HCMNEXT_EMBEDDING_DIR (or HCMNEXT_EMBEDDING_URL and
//	HCMNEXT_EMBEDDING_MODEL); -drain also runs the indexer until done
//	migrate seed    load the deterministic Promotion fixture for -tenant
//	migrate demo-people load HarborCare's demo workforce and processed photos
//	migrate upgrade drive one rolling schema/binary upgrade through the
//	durable upgrade journal (-upgrade-plan, -upgrade-rows, -journal-path,
//	-required-watermark); re-running the same command resumes after a kill
//
// migrate is not a long-running server: process-roles.yaml marks it
// "operator-invoked" with no readiness probe and "not applicable" drain, so
// this composition root serves no health endpoint and Build returns a
// single Workload that runs once and returns rather than looping. Unlike
// every other role's settings, the up|down|status subcommand is a plain
// positional argument (not a -flag), read directly from os.Args before
// bootstrap.Run ever parses Spec.ConfigFields - the same way bootstrap.Run
// itself pre-empts Spec.HealthAddr resolution in cmd/worker and
// cmd/projector.
//
// The target server is HCMNEXT_DATABASE_URL, overridable with
// -database-url. The chat subcommands target the independent chat database at
// HCMNEXT_CHAT_DATABASE_URL, overridable with -chat-database-url, and refuse to
// run when that resolves to the same database as the core DSN.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/chatroutestore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
)

// EnvDatabaseURL names the server this command migrates, matching
// cmd/worker's and cmd/projector's convention.
const EnvDatabaseURL = "HCMNEXT_DATABASE_URL"

const fieldTenant = "tenant"

const (
	fieldUpgradeJournal   = "journal-path"
	fieldUpgradePlan      = "upgrade-plan"
	fieldUpgradeRows      = "upgrade-rows"
	fieldUpgradeWatermark = "required-watermark"
)

const (
	fieldPhotoSource = "photo-source"
	// The chat seed's own flags. They live beside the other seed flags because
	// bootstrap declares one flag set per role, not one per subcommand.
	fieldChatSeedScale = "scale"
	fieldChatSeedReset = "reset"
	// fieldDocumentEmbedDrain makes `document embed` run the indexer in
	// process until the queue is empty.
	fieldDocumentEmbedDrain = "drain"
	fieldChatSeedMediaRoot  = "chat-media-root"
	fieldAssetDir           = "asset-dir"
	fieldOriginalDir        = "original-dir"
)

// migrationTimeout bounds one migrate invocation, matching the original
// command's own budget. Unlike the original (which ran against a bare
// context.Background(), deaf to OS signals), this timeout is derived from
// bootstrap's own workload context, so a SIGINT/SIGTERM during a long
// migration now cancels it too rather than only a plain deadline.
const migrationTimeout = 30 * time.Minute

func main() {
	command, rest := splitCommand(os.Args[1:])
	os.Exit(bootstrap.Run(context.Background(), spec(command, rest)))
}

// splitCommand peels the leading positional subcommand off args, matching
// migrate's "migrate up|down|status [flags...]" invocation. An empty args
// yields an empty command, which validateCommand below reports as a usage
// error.
func splitCommand(args []string) (command string, rest []string) {
	if len(args) == 0 {
		return "", nil
	}
	// "chat" is a namespace, not an action: the chat schema lives on its own
	// database with its own migration set, so its action is the second token.
	if args[0] == "chat" {
		if len(args) == 1 {
			return chatCommandPrefix, nil
		}
		return chatCommandPrefix + args[1], args[2:]
	}
	if args[0] == "document" {
		if len(args) == 1 {
			return documentCommandPrefix, nil
		}
		return documentCommandPrefix + args[1], args[2:]
	}
	return args[0], args[1:]
}

// migrateConfigFields declares every flag/env-backed value this role
// accepts: only the database URL - migrate has no health endpoint, poll
// interval or other tunable.
func migrateConfigFields() []bootstrap.Field {
	return []bootstrap.Field{
		{
			Name:   "database-url",
			Env:    EnvDatabaseURL,
			Usage:  "PostgreSQL connection URL (" + EnvDatabaseURL + " if unset)",
			Kind:   bootstrap.KindString,
			Secret: true,
		},
		{
			Name:   fieldChatDatabaseURL,
			Env:    EnvChatDatabaseURL,
			Usage:  "PostgreSQL connection URL for the independent chat database (" + EnvChatDatabaseURL + " if unset)",
			Kind:   bootstrap.KindString,
			Secret: true,
		},
		{
			Name:   fieldDocumentDatabaseURL,
			Env:    EnvDocumentDatabaseURL,
			Usage:  "PostgreSQL connection URL for the independent document database (" + EnvDocumentDatabaseURL + " if unset)",
			Kind:   bootstrap.KindString,
			Secret: true,
		},
		{
			Name:  fieldTenant,
			Usage: "tenant slug to seed (required by seed and demo-people)",
		},
		{
			Name:  fieldPhotoSource,
			Usage: "directory containing generated hc-NNN.png source photos (required by demo-people)",
		},
		{
			Name:    fieldChatSeedScale,
			Usage:   "chat seed profile: small or full",
			Default: "full",
		},
		{
			Name:    fieldDocumentEmbedDrain,
			Usage:   "document embed: run the indexer in process until the queue is empty",
			Kind:    bootstrap.KindBool,
			Default: "false",
		},
		{
			Name:    fieldChatSeedReset,
			Usage:   "chat seed: wipe and recreate the demo rooms instead of refusing when they exist",
			Kind:    bootstrap.KindBool,
			Default: "false",
		},
		{
			Name:    fieldChatSeedMediaRoot,
			Usage:   "chat seed: durable root for uploaded media bytes",
			Default: defaultArtifactRootPath + "/chat-media",
		},
		{
			Name:    fieldAssetDir,
			Usage:   "workspace asset directory for retained originals and display proxies",
			Default: "internal/humanwork/workspace/assets",
		},
		{
			Name:    fieldOriginalDir,
			Usage:   "non-public directory for byte-exact retained profile-photo originals",
			Default: "demo-assets/profile-originals",
		},
		{
			Name:  fieldUpgradeJournal,
			Usage: "durable upgrade journal file for the upgrade subcommand (resume-aware)",
		},
		{
			Name:  fieldUpgradePlan,
			Usage: "JSON file carrying the schemaupgrade plan for the upgrade subcommand",
		},
		{
			Name:  fieldUpgradeRows,
			Usage: "JSON file carrying the source rows for the upgrade subcommand",
		},
		{
			Name:    fieldUpgradeWatermark,
			Usage:   "consumer adoption watermark the upgrade subcommand cuts over at",
			Kind:    bootstrap.KindInt,
			Default: "0",
		},
	}
}

// spec builds the full migrate Spec for one invocation's subcommand and
// remaining flag arguments.
func spec(command string, rest []string) bootstrap.Spec {
	return bootstrap.Spec{
		Role:         bootstrap.RoleMigrate,
		Args:         rest,
		ConfigFields: append(migrateConfigFields(), documentPruneFields()...),
		Validate:     validateConfig(command),
		// No DatabaseURLField/DBPoolFactory: Goose and schema.Journal both
		// need a database/sql.DB (via pgx's stdlib adapter), not
		// bootstrap's narrower DBPool port, so this role opens its own
		// connection inside the Workload (openMigrateDB in migrate.go)
		// instead. The seed workload opens its own pgx adapter connection for
		// the same reason: it needs the transaction-scoped dbport adapter, not
		// bootstrap's narrower DBPool port.
		// No HealthAddr: migrate is operator-invoked and short-lived
		// (process-roles.yaml: "not applicable" liveness/readiness,
		// drain_policy "not applicable"), so it serves no health endpoint.
		Build: func(_ context.Context, deps bootstrap.Deps) (bootstrap.Runtime, error) {
			url := deps.Values.String("database-url")
			wl := bootstrap.Workload{
				Name: "migrate-" + command,
				Run: func(ctx context.Context) error {
					ctx, cancel := context.WithTimeout(ctx, migrationTimeout)
					defer cancel()

					if command == "seed" || command == "demo-people" {
						conn, err := openSeedDB(ctx, url)
						if err != nil {
							return err
						}
						defer func() { _ = conn.Close(ctx) }()

						if command == "demo-people" {
							return runDemoPeopleCommand(ctx, conn, deps.Values.String(fieldTenant), deps.Values.String(fieldPhotoSource), deps.Values.String(fieldAssetDir), deps.Values.String(fieldOriginalDir), os.Stdout)
						}
						return runSeedCommand(ctx, conn, deps.Values.String(fieldTenant), os.Stdout)
					}

					if action := chatSubcommand(command); action != "" || command == chatCommandPrefix {
						if action == "seed" {
							// The seeder needs the store's own pooled adapter,
							// not the database/sql handle Goose migrates with.
							store, storeErr := chatstore.New(ctx, chatstore.Config{DSN: deps.Values.String(fieldChatDatabaseURL)})
							if storeErr != nil {
								return storeErr
							}
							defer store.Close()
							// Seeded rooms must be registered in the core route
							// directory the same way a live CreateConversation
							// call registers them (composeChatRouting), or the
							// routed service refuses every reaction, read-state
							// and other lease-guarded write against them
							// forever (CHAT-04). The directory lives in the core
							// database, the "-database-url" this role already
							// resolves for Goose -- not the chat database.
							routeConn, routeErr := openSeedDB(ctx, url)
							if routeErr != nil {
								return routeErr
							}
							defer func() { _ = routeConn.Close(ctx) }()
							routes, routesErr := chatroutestore.New(routeConn)
							if routesErr != nil {
								return routesErr
							}
							if routesErr = routes.Migrate(ctx); routesErr != nil {
								return routesErr
							}
							reset, resetErr := deps.Values.Bool(fieldChatSeedReset)
							if resetErr != nil {
								return resetErr
							}
							return runChatSeedCommand(ctx, store, routes, chatSeedOptions{
								Tenant:    deps.Values.String(fieldTenant),
								Scale:     deps.Values.String(fieldChatSeedScale),
								Reset:     reset,
								MediaRoot: deps.Values.String(fieldChatSeedMediaRoot),
								AssetDir:  deps.Values.String(fieldAssetDir),
							}, os.Stdout)
						}
						db, err := openChatMigrateDB(ctx, deps.Values.String(fieldChatDatabaseURL))
						if err != nil {
							return err
						}
						defer func() { _ = db.Close() }()

						return runChatMigrateCommand(ctx, action, db, os.Stdout)
					}
					if action := documentSubcommand(command); action != "" || command == documentCommandPrefix {
						if action == "seed" {
							return runDocumentSeedAction(ctx, deps.Values.String(fieldDocumentDatabaseURL), url, deps.Values.String(fieldChatDatabaseURL), deps.Values.String(fieldTenant), os.Stdout)
						}
						if action == "prune" {
							return runDocumentPruneAction(ctx, deps.Values, url, os.Stdout)
						}
						if action == "embed" {
							drain, drainErr := deps.Values.Bool(fieldDocumentEmbedDrain)
							if drainErr != nil {
								return drainErr
							}
							return runDocumentEmbedAction(ctx, deps.Values.String(fieldDocumentDatabaseURL), url, deps.Values.String(fieldChatDatabaseURL), deps.Values.String(fieldTenant), drain, os.Getenv, os.Stdout)
						}
						db, err := openDocumentMigrateDB(ctx, deps.Values.String(fieldDocumentDatabaseURL))
						if err != nil {
							return err
						}
						defer func() { _ = db.Close() }()
						return runDocumentMigrateCommand(ctx, action, db, os.Stdout)
					}

					if command == "upgrade" {
						watermark, err := deps.Values.Int(fieldUpgradeWatermark)
						if err != nil {
							return err
						}
						if watermark < 0 {
							return fmt.Errorf("-%s must not be negative", fieldUpgradeWatermark)
						}
						return runUpgradeCommand(
							deps.Values.String(fieldUpgradeJournal),
							deps.Values.String(fieldUpgradePlan),
							deps.Values.String(fieldUpgradeRows),
							uint64(watermark),
							os.Stdout,
						)
					}

					db, err := openMigrateDB(ctx, url)
					if err != nil {
						return err
					}
					defer func() { _ = db.Close() }()

					return runMigrateCommand(ctx, command, db, os.Stdout)
				},
			}
			return bootstrap.Runtime{Workloads: []bootstrap.Workload{wl}}, nil
		},
	}
}

// validateConfig fails config resolution (before the Workload ever opens a
// connection) on an unrecognized subcommand or a missing database URL,
// matching the original command's own usage checks.
func validateConfig(command string) func(*bootstrap.Values) error {
	return func(v *bootstrap.Values) error {
		if strings.HasPrefix(command, chatCommandPrefix) {
			// The chat subcommands never touch the core database, so they are
			// validated against the chat DSN alone plus the isolation rule.
			return validateChatCommand(chatSubcommand(command), v.String(fieldChatDatabaseURL), v.String("database-url"))
		}
		if strings.HasPrefix(command, documentCommandPrefix) {
			return validateDocumentCommand(documentSubcommand(command), v.String(fieldDocumentDatabaseURL), v.String("database-url"), v.String(fieldChatDatabaseURL))
		}
		switch command {
		case "up", "down", "status":
		case "seed":
			if v.String(fieldTenant) == "" {
				return fmt.Errorf("-%s is required for the seed subcommand", fieldTenant)
			}
		case "demo-people":
			if v.String(fieldTenant) == "" {
				return fmt.Errorf("-%s is required for the demo-people subcommand", fieldTenant)
			}
			if v.String(fieldPhotoSource) == "" {
				return fmt.Errorf("-%s is required for the demo-people subcommand", fieldPhotoSource)
			}
		case "upgrade":
			if v.String(fieldUpgradeJournal) == "" {
				return fmt.Errorf("-%s is required for the upgrade subcommand", fieldUpgradeJournal)
			}
			if v.String(fieldUpgradePlan) == "" {
				return fmt.Errorf("-%s is required for the upgrade subcommand", fieldUpgradePlan)
			}
			if v.String(fieldUpgradeRows) == "" {
				return fmt.Errorf("-%s is required for the upgrade subcommand", fieldUpgradeRows)
			}
			// The upgrade subcommand journals to a file and never opens a
			// database, so it is validated without a database URL.
			return nil
		case "":
			return fmt.Errorf("usage: migrate up|down|status|seed|demo-people|upgrade|chat up|chat status|document up|document status|document seed|document embed")
		default:
			return fmt.Errorf("unknown command %q; usage: migrate up|down|status|seed|demo-people|upgrade|chat up|chat status|document up|document status|document seed|document embed", command)
		}
		if v.String("database-url") == "" {
			return fmt.Errorf("%s is not set; pass -database-url or set the environment variable", EnvDatabaseURL)
		}
		return nil
	}
}
