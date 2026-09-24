// Command modelgen generates the SchemaFlux model package from the checked-in
// source tree. It is intentionally deterministic and offline.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/monstercameron/human-capital-management-suite/tools/gen/schemaflux/modelgen"
	"github.com/monstercameron/human-capital-management-suite/tools/gen/schemaflux/sources"
)

func main() {
	root := flag.String("root", ".", "repository root")
	out := flag.String("out", "tools/gen/schemaflux/generated/models_generated.go", "generated Go file")
	flag.Parse()
	metamodel, err := sources.LoadMetamodel(filepath.Join(*root, "schema/schemaflux/metamodel/v1/metamodel.yaml"))
	must(err)
	auth, ret, err := sources.LoadRegistries(filepath.Join(*root, "schema/schemaflux/registries/v1/registries.yaml"))
	must(err)
	ents, rels, err := sources.LoadEntityFamilies(filepath.Join(*root, "schema/schemaflux/entities/v1"))
	must(err)
	manifest, errs := sources.Compile(sources.Bundle{Metamodel: metamodel, Authorities: auth, Retentions: ret, Entities: ents, Relationships: rels})
	if len(errs) != 0 {
		for _, e := range errs {
			fmt.Fprintln(os.Stderr, e)
		}
		os.Exit(1)
	}
	a, err := modelgen.Generate(manifest, "schemaflux")
	must(err)
	if err := os.MkdirAll(filepath.Dir(filepath.Join(*root, *out)), 0o755); err != nil {
		must(err)
	}
	must(os.WriteFile(filepath.Join(*root, *out), a.Go, 0o644))
	fmt.Printf("generated %s\nsource digest: %s\ngenerated digest: %s\n", *out, a.SourceDigest, a.GeneratedDigest)
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
