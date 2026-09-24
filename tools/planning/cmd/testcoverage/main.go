// Command testcoverage regenerates or checks the per-file Go coverage logs.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/testcoverage"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "testcoverage:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("testcoverage", flag.ContinueOnError)
	root := flags.String("root", ".", "repository root")
	write := flags.Bool("write", false, "generate inventory logs or a package checkpoint")
	moduleName := flags.String("module", "", "module name for a single-package coverage run")
	packagePath := flags.String("package", "", "measure exactly one Go import path and save its checkpoint")
	packageList := flags.String("package-list", "", "newline-delimited import paths to checkpoint as one batch")
	workers := flags.Int("workers", 1, "bounded concurrent package test processes (1-4) for -package-list")
	outputPath := flags.String("output", "", "write a module inventory to a specific repository path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if *outputPath != "" && (*packagePath != "" || *packageList != "") {
		return fmt.Errorf("-output cannot be combined with -package or -package-list")
	}
	if *packagePath != "" || *packageList != "" {
		if !*write {
			return fmt.Errorf("-package or -package-list requires -write")
		}
		if *packagePath != "" && *packageList != "" {
			return fmt.Errorf("-package and -package-list cannot be combined")
		}
		if *packagePath != "" && *workers != 1 {
			return fmt.Errorf("-workers requires -package-list")
		}
		var selected *testcoverage.Module
		for i := range testcoverage.Modules {
			if testcoverage.Modules[i].Name == *moduleName {
				selected = &testcoverage.Modules[i]
				break
			}
		}
		if selected == nil {
			return fmt.Errorf("-package or -package-list requires -module root or nested")
		}
		if *packagePath != "" {
			return testcoverage.CheckpointPackage(ctx, *root, *selected, *packagePath)
		}
		data, err := os.ReadFile(*packageList)
		if err != nil {
			return fmt.Errorf("read package list: %w", err)
		}
		var paths []string
		for _, line := range strings.Split(string(data), "\n") {
			path := strings.TrimSpace(line)
			if path != "" && !strings.HasPrefix(path, "#") {
				paths = append(paths, path)
			}
		}
		return testcoverage.CheckpointPackagesWithWorkers(ctx, *root, *selected, paths, *workers)
	}
	if *moduleName != "" {
		if !*write {
			return fmt.Errorf("-module requires -write")
		}
		for _, module := range testcoverage.Modules {
			if module.Name == *moduleName {
				return testcoverage.WriteModuleTo(ctx, *root, module, *outputPath)
			}
		}
		return fmt.Errorf("unknown module %q; want root or nested", *moduleName)
	}
	if *outputPath != "" {
		return fmt.Errorf("-output requires -module root or nested")
	}
	if *write {
		return testcoverage.Write(ctx, *root, testcoverage.Modules)
	}
	return testcoverage.Check(ctx, *root, testcoverage.Modules)
}
