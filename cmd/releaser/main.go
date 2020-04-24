package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"sort"
	"strings"

	semver "github.com/Masterminds/semver/v3"
)

const (
	rcBranchFmt      = "rc/v%s"
	preRcBranchFmt   = "pre-rc/v%s"
	releaseBranchFmt = "release/v%s"
)

var (
	goCmd  string
	gitCmd string

	//major = flag.Bool("major", false, "this is a major release")
)

func init() {
	log.SetFlags(0)
	log.SetPrefix("releaser: ")
	gitCmd = lookupCmd("git")
	goCmd = lookupCmd("go")
}

func main() {
	flag.Parse()

	if flag.NArg() != 2 {
		log.Fatal("invalid number of arguments")
	}

	switch flag.Arg(0) {
	case "start":
		startPointRelease(semver.MustParse(flag.Arg(1)))
		return
	default:
		log.Fatal("unknown command")
	}
}

func startPointRelease(releaseVer *semver.Version) {
	rcbranch := fmt.Sprintf(rcBranchFmt, releaseVer)
	currentBranch := execCombineOutput(gitCmd, "branch", "--show-current")
	if currentBranch == rcbranch {
		log.Fatalf("Already on %q\n", rcbranch)
	}

	allReleases := []*semver.Version{releaseVer}
	tags := strings.Split(execCombineOutput(gitCmd, "tag", "-l"), "\n")
	for _, tag := range tags {
		tag = strings.TrimPrefix(tag, "v")
		semtag, err := semver.StrictNewVersion(tag)
		if err != nil {
			continue // ignore invalid tags
		}
		if semtag.Equal(releaseVer) {
			log.Fatalln("relase tag already exists")
		}
		allReleases = append(allReleases, semtag)
	}

	sort.Sort(semver.Collection(allReleases))

	var prevRel *semver.Version
	for _, rel := range allReleases {
		next := rel.IncPatch()
		if releaseVer.Equal(&next) {
			prevRel = rel
		}
	}

	if prevRel == nil {
		log.Fatal("couldn't find previous release")
	}

	baseBranch := fmt.Sprintf(releaseBranchFmt, prevRel)
	prercbranch := fmt.Sprintf(preRcBranchFmt, releaseVer)
	_ = execCombineOutput(gitCmd, "checkout", baseBranch)
	_ = execCombineOutput(gitCmd, "checkout", "-b", rcbranch)
	_ = execCombineOutput(gitCmd, "checkout", "-b", prercbranch)
	log.Printf("1. cherry pick commits 2. add entries to CHANGELOG 3. open a PR merging %q into %q\n", prercbranch, rcbranch)
}

func execCombineOutput(cmd string, args ...string) string {
	log.Println("exec:", cmd, strings.Join(args, " "))
	out, err := exec.Command(cmd, args...).CombinedOutput()
	if err != nil {
		log.Fatalf("command '%s %s' failed: %v\n", cmd, strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out))
}

func lookupCmd(cmd string) string {
	path, err := exec.LookPath(cmd)
	if err != nil {
		log.Fatal(fmt.Errorf("couldn't find %s: %w", cmd, err))
	}
	if _, err := os.Stat(path); err != nil {
		log.Fatal(err)
	}
	return path
}
