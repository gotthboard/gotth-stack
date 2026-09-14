package main

import (
	"fmt"
	"io"
	"os"

	"github.com/gotthboard/gotth-stack/pkg/stack"
)

const maxPathBytes = 4096

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(arguments []string, stdout, stderr io.Writer) int {
	if len(arguments) != 2 || (arguments[0] != "validate" && arguments[0] != "plan") {
		_, _ = fmt.Fprintln(stderr, "usage: gotth-stack <validate|plan> <manifest.json>")
		return 2
	}
	manifest, err := readManifest(arguments[1])
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "gotth-stack: manifest rejected")
		return 1
	}
	plan, err := stack.BuildPlan(manifest)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "gotth-stack: manifest rejected")
		return 1
	}
	if arguments[0] == "validate" {
		if _, err := fmt.Fprintf(stdout, "valid %s\n", plan.ManifestDigest); err != nil {
			_, _ = fmt.Fprintln(stderr, "gotth-stack: output failed")
			return 1
		}
		return 0
	}
	encoded, err := stack.MarshalPlan(plan)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "gotth-stack: plan rejected")
		return 1
	}
	if _, err := stdout.Write(encoded); err != nil {
		_, _ = fmt.Fprintln(stderr, "gotth-stack: output failed")
		return 1
	}
	return 0
}

func readManifest(path string) (stack.Manifest, error) {
	if len(path) < 1 || len(path) > maxPathBytes {
		return stack.Manifest{}, stack.ErrInvalidJSON
	}
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() {
		return stack.Manifest{}, stack.ErrInvalidJSON
	}
	file, err := os.Open(path)
	if err != nil {
		return stack.Manifest{}, stack.ErrInvalidJSON
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil || !after.Mode().IsRegular() || !os.SameFile(before, after) || after.Size() > stack.MaxManifestBytes {
		return stack.Manifest{}, stack.ErrInvalidJSON
	}
	return stack.ParseManifest(file)
}
