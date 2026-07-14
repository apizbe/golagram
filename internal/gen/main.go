package main

import (
	"fmt"
	"go/format"
	"os"
	"path/filepath"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "gen:", err)
		os.Exit(1)
	}
}

func run() error {
	// go run/go generate keep the invoking shell's working directory, and
	// both are documented (spec.go) to be run from the repo root.
	root, err := filepath.Abs(".")
	if err != nil {
		return err
	}

	data, err := os.ReadFile(filepath.Join(root, "scripts", "api.json"))
	if err != nil {
		return fmt.Errorf("reading scripts/api.json: %w", err)
	}

	spec, err := parseSpec(data)
	if err != nil {
		return fmt.Errorf("parsing scripts/api.json: %w", err)
	}

	// Build the union lookup tables (discriminators, decodability) — shared
	// with gen_test.go's determinism test so the two can't drift.
	if err := prepareSpec(spec); err != nil {
		return err
	}

	if err := writeGenerated(filepath.Join(root, "types.gen.go"), renderTypesFile(spec.APIVersion, spec.Items)); err != nil {
		return err
	}
	if err := writeGenerated(filepath.Join(root, "methods.gen.go"), renderMethodsFile(spec.APIVersion, spec.Items)); err != nil {
		return err
	}

	enumsJSON, err := os.ReadFile(filepath.Join(root, "internal", "gen", "enums.json"))
	if err != nil {
		return fmt.Errorf("reading internal/gen/enums.json: %w", err)
	}
	groups, err := buildEnumGroups(spec.Items, enumsJSON)
	if err != nil {
		return err
	}
	if err := writeGenerated(filepath.Join(root, "consts.gen.go"), renderConstsFile(spec.APIVersion, groups)); err != nil {
		return err
	}

	return nil
}

// writeGenerated gofmt's source and writes it to path.
func writeGenerated(path, source string) error {
	formatted, err := formatOrRaw(source)
	if err != nil {
		// Write the unformatted source anyway so it's inspectable — a
		// generator that silently drops its output on a formatting error is
		// its own kind of fake.
		_ = os.WriteFile(path, []byte(source), 0o644)
		return fmt.Errorf("gofmt %s: %w (unformatted output written for inspection)", path, err)
	}
	return os.WriteFile(path, []byte(formatted), 0o644)
}

// formatOrRaw runs gofmt over generated source, also used by gen_test.go to
// compare freshly generated output against what's committed.
func formatOrRaw(source string) (string, error) {
	formatted, err := format.Source([]byte(source))
	if err != nil {
		return "", err
	}
	return string(formatted), nil
}
