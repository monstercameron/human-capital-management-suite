package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseArgs_RequiresExactlyOneMode(t *testing.T) {
	for _, args := range [][]string{{}, {"-changed", "-all"}, {"-all", "-pkg", "./x"}} {
		if _, err := parseArgs(args); err == nil {
			t.Errorf("args %v must be refused", args)
		}
	}
	o, err := parseArgs([]string{"-root", "r", "-pkg", "./a", "-pkg", "./b", "-timeout", "1m"})
	if err != nil || o.root != "r" || len(o.pkgs) != 2 || o.timeout.Minutes() != 1 {
		t.Fatalf("parsed %+v, %v", o, err)
	}
	if _, err := parseArgs([]string{"-bogus"}); err == nil {
		t.Fatal("unknown flag must be refused")
	}
	for _, args := range [][]string{
		{"-all", "-shard-count", "0"},
		{"-all", "-shard-count", "4", "-shard-index", "4"},
		{"-pkg", "./x", "-shard-count", "4"},
	} {
		if _, err := parseArgs(args); err == nil {
			t.Errorf("invalid shard args %v must be refused", args)
		}
	}
	if o, err := parseArgs([]string{"-all", "-shard-count", "4", "-shard-index", "2"}); err != nil || o.shardCount != 4 || o.shardIndex != 2 {
		t.Fatalf("valid shard args parsed as %+v, %v", o, err)
	}
}

func TestShardPackages_PartitionsEveryPackageExactlyOnce(t *testing.T) {
	pkgs := []string{"a", "b", "c", "d", "e", "f", "g"}
	seen := map[string]int{}
	for shard := 0; shard < 4; shard++ {
		for _, pkg := range shardPackages(pkgs, shard, 4) {
			seen[pkg]++
		}
	}
	if len(seen) != len(pkgs) {
		t.Fatalf("partition covered %d packages, want %d: %v", len(seen), len(pkgs), seen)
	}
	for _, pkg := range pkgs {
		if seen[pkg] != 1 {
			t.Fatalf("package %q assigned %d times, want once", pkg, seen[pkg])
		}
	}
}

func TestRun_ReportsUsageErrorsAndGatesARealPackage(t *testing.T) {
	var out bytes.Buffer
	if code := run([]string{"-changed", "-all"}, &out); code != 2 || !strings.Contains(out.String(), "exactly one") {
		t.Fatalf("usage error: code=%d out=%q", code, out.String())
	}
	root := repoRoot(t)
	out.Reset()
	code := run([]string{"-root", root, "-pkg", "./tools/quality/covergate/testdata/gatedpkg", "-timeout", "5m"}, &out)
	if code != 0 || !strings.Contains(out.String(), "PASS") {
		t.Fatalf("real gate run: code=%d out=%q", code, out.String())
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test directory")
		}
		dir = parent
	}
}
