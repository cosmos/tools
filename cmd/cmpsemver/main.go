package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/Masterminds/semver/v3"
)

var (
	progName string
	verbose bool
)

func init() {
	progName = filepath.Base(os.Args[0])

	log.SetPrefix(fmt.Sprintf("%s: ", progName))
	log.SetFlags(0)
	log.SetOutput(os.Stderr)
	flag.Usage = help
	flag.BoolVar(&verbose, "v", false, "verbose mode.")
}

func main() {
	flag.Parse()

	if flag.NArg() != 3 {
		log.Fatalf("takes three arguments: <version> <relation> <version>")
	}

	ver1, err := semver.StrictNewVersion(flag.Arg(0))
	if err != nil {
		log.Fatalf("failed to parse %s: %v", flag.Arg(0), err)
	}

	ver2, err := semver.StrictNewVersion(flag.Arg(2))
	if err != nil {
		log.Fatalf("failed to parse %s: %v", flag.Arg(2), err)
	}

	exitSuccess, err := compareVersions(ver1, ver2, flag.Arg(1))
	if err != nil {
		log.Fatal(err)
	}

	if verbose {
		fmt.Fprintln(log.Writer(), exitSuccess)
	}

	if !exitSuccess {
		os.Exit(1)
	}
}

func compareVersions(ver1, ver2 *semver.Version, op string) (bool, error) {
	switch op {
	case "lt":
		if !ver1.LessThan(ver2) {
			return false, nil
		}
	case "le":
		if ver1.GreaterThan(ver2) {
			return false, nil
		}
	case "eq":
		if !ver1.Equal(ver2) {
			return false, nil
		}
	case "ne":
		if ver1.Equal(ver2) {
			return false, nil
		}
	case "ge":
		if ver1.LessThan(ver2) {
			return false, nil
		}
	case "gt":
		if !ver1.GreaterThan(ver2) {
			return false, nil
		}
	default:
		return false, fmt.Errorf("invalid operator %q", op)
	}

	return true, nil
}

func help() {
	fmt.Fprintf(os.Stderr, "Usage: %s VER1 OP VER2\n", progName)
	fmt.Fprintf(os.Stderr, "Compare version numbers, where OP is a binary operator.\n\n")
	fmt.Fprintf(os.Stderr, "%s returns true (0) if the specified condition is satisfied,\n"+
		"and false (1) otherwise.\n\nOperators: lt le eq ne ge gt\n\n", progName)
	fmt.Fprintln(os.Stderr, "Options:")
	flag.PrintDefaults()
}
