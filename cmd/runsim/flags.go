package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

var (
	genesis, blocks, period, simId, hostId, logObjPrefix string

	pkgName          = "./simapp"
	seedOverrideList = ""

	notifySlack, notifyGithub, exitOnFail bool
)

func initFlags() {
	flag.StringVar(&genesis, "Genesis", "", "genesis file path")
	flag.StringVar(&pkgName, "SimAppPkg", "github.com/cosmos/cosmos-sdk/simapp", "sim app package")
	flag.StringVar(&simId, "SimId", "", "long sim ID")
	flag.StringVar(&hostId, "HostId", "", "long sim host ID")
	flag.StringVar(&seedOverrideList, "Seeds", "", "override default seeds with comma-separated list")
	flag.StringVar(&logObjPrefix, "LogObjPrefix", "", "the S3 object prefix used when uploading logs")
	flag.BoolVar(&notifySlack, "Slack", false, "report results to Slack channel")
	flag.BoolVar(&notifyGithub, "Github", false, "update github check")
	flag.BoolVar(&exitOnFail, "ExitOnFail", false, "exit on fail during multi-sim, print error")
	flag.IntVar(&jobs, "Jobs", jobs, "number of parallel processes")
	flag.DurationVar(&timeout, "Timeout", defaultTimeout, "simulations fail if they run longer than the supplied timeout")

	flag.Usage = func() {
		_, _ = fmt.Fprintf(flag.CommandLine.Output(),
			"Usage: %s [-Jobs maxprocs] [-ExitOnFail] [-Seeds comma-separated-seed-list] [-Genesis file-path] "+
				"[-SimAppPkg file-path] [-Github] [-Slack] [-LogObjPrefix string] [blocks] [period] [testname]\n"+
				"Run simulations in parallel\n", filepath.Base(os.Args[0]))
		flag.PrintDefaults()
	}
}
