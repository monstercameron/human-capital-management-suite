// Command covergate runs the unit-test and coverage floor gate.
//
//	go run ./tools/quality/covergate/cmd/covergate -root . -changed   # packages with staged Go files (pre-commit)
//	go run ./tools/quality/covergate/cmd/covergate -root . -all       # every package (CI)
//	go run ./tools/quality/covergate/cmd/covergate -root . -pkg ./internal/trust/authz -pkg ./internal/authn
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/monstercameron/human-capital-management-suite/tools/quality/covergate"
)

type options struct {
	root       string
	config     string
	changed    bool
	all        bool
	pkgs       []string
	timeout    time.Duration
	shardIndex int
	shardCount int
}

type pkgList []string

func (p *pkgList) String() string     { return fmt.Sprint([]string(*p)) }
func (p *pkgList) Set(v string) error { *p = append(*p, v); return nil }

func parseArgs(args []string) (options, error) {
	fs := flag.NewFlagSet("covergate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var o options
	var pkgs pkgList
	fs.StringVar(&o.root, "root", ".", "repository root")
	fs.StringVar(&o.config, "config", covergate.DefaultConfigPath, "policy file relative to root")
	fs.BoolVar(&o.changed, "changed", false, "gate the packages holding staged Go files")
	fs.BoolVar(&o.all, "all", false, "gate every package in the root module")
	fs.DurationVar(&o.timeout, "timeout", 30*time.Minute, "go test timeout")
	fs.IntVar(&o.shardIndex, "shard-index", 0, "zero-based shard index (only with -all)")
	fs.IntVar(&o.shardCount, "shard-count", 1, "number of deterministic shards (only with -all)")
	fs.Var(&pkgs, "pkg", "package pattern to gate (repeatable)")
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	o.pkgs = pkgs
	modes := 0
	for _, on := range []bool{o.changed, o.all, len(o.pkgs) > 0} {
		if on {
			modes++
		}
	}
	if modes != 1 {
		return o, fmt.Errorf("choose exactly one of -changed, -all or -pkg")
	}
	if o.shardCount < 1 || o.shardIndex < 0 || o.shardIndex >= o.shardCount {
		return o, fmt.Errorf("shard index %d must be in [0, %d)", o.shardIndex, o.shardCount)
	}
	if !o.all && (o.shardCount != 1 || o.shardIndex != 0) {
		return o, fmt.Errorf("sharding is supported only with -all")
	}
	return o, nil
}

func shardPackages(pkgs []string, index, count int) []string {
	if count == 1 {
		return pkgs
	}
	shard := make([]string, 0, (len(pkgs)+count-1)/count)
	for i, pkg := range pkgs {
		if i%count == index {
			shard = append(shard, pkg)
		}
	}
	return shard
}

func run(args []string, stdout io.Writer) int {
	o, err := parseArgs(args)
	if err != nil {
		fmt.Fprintln(stdout, "covergate:", err)
		return 2
	}
	pkgs := o.pkgs
	switch {
	case o.changed:
		pkgs, err = covergate.StagedPackages(o.root)
	case o.all:
		pkgs, err = covergate.AllPackages(o.root)
		pkgs = shardPackages(pkgs, o.shardIndex, o.shardCount)
	}
	if err != nil {
		fmt.Fprintln(stdout, err)
		return 2
	}
	if len(pkgs) == 0 {
		fmt.Fprintln(stdout, "covergate: PASS no Go packages to gate")
		return 0
	}
	report, err := covergate.Gate(o.root, o.config, pkgs, time.Now(), o.timeout)
	if err != nil {
		fmt.Fprintln(stdout, err)
		return 2
	}
	fmt.Fprint(stdout, report.Format())
	if len(report.Findings) > 0 {
		return 1
	}
	return 0
}

func main() { os.Exit(run(os.Args[1:], os.Stdout)) }
