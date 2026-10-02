package chatload

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Command runs the CLI with explicit arguments and output, keeping flags and
// test-server configuration local to the invocation.
func Command(ctx context.Context, args []string, out io.Writer, environmentDSN string) error {
	flags := flag.NewFlagSet("chatload", flag.ContinueOnError)
	flags.SetOutput(out)
	c := Config{}
	flags.StringVar(&c.Root, "root", ".", "repository root")
	flags.StringVar(&c.DSN, "dsn", "", "shared test server DSN (prefer environment)")
	flags.StringVar(&c.Database, "database", "", "explicit test database name, for one size")
	flags.BoolVar(&c.Resume, "resume", false, "resume a matching database after an abrupt termination; drop after measurement")
	flags.Uint64Var(&c.Seed, "seed", 20261001, "deterministic seed")
	flags.IntVar(&c.Tenants, "tenants", 200, "tenant count")
	flags.IntVar(&c.Channels, "channels", 100000, "channel count, at least 300 per tenant")
	flags.IntVar(&c.Batch, "batch", 5000, "atomic COPY batch size")
	flags.IntVar(&c.Repeats, "repeats", 20, "EXPLAIN repetitions")
	flags.IntVar(&c.Concurrency, "concurrency", 8, "mixed workload workers")
	flags.IntVar(&c.Operations, "operations", 160, "mixed workload operations")
	sizes := flags.String("sizes", "100000,1000000,3000000", "post counts, at most 3 million")
	status := flags.Bool("status", false, "show only chatscale sessions on shared test server")
	refresh := flags.Bool("refresh-report", false, "rebuild classifications from saved measurements, without database access")
	cancel := flags.Bool("cancel", false, "cancel SQL in one named matching measurement database, without terminating backends")
	if e := flags.Parse(args); e != nil {
		return e
	}
	if c.DSN == "" {
		c.DSN = environmentDSN
	}
	if *refresh {
		return RefreshReport(c.Root)
	}
	if *cancel {
		c.Posts = 5
		n, e := CancelMeasurement(context.Background(), c)
		fmt.Fprintf(out, "cancelled %d active statements\n", n)
		return e
	}
	if *status {
		rows, e := Status(context.Background(), c)
		for _, row := range rows {
			fmt.Fprintln(out, row)
		}
		return e
	}
	var targets []int64
	for _, v := range strings.Split(*sizes, ",") {
		n, e := strconv.ParseInt(v, 10, 64)
		if e != nil {
			return e
		}
		targets = append(targets, n)
	}

	r, e := Run(ctx, c, targets)
	writeErr := WriteReport(c.Root, r)
	if e != nil {
		return e
	}
	return writeErr
}
