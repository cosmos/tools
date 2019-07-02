package main

// This software is
//
//  Copyright (C) 2019 Alessio Treglia <alessio@tendermint.com>
//  Copyright (C) 2019 Rigel Rozanski <rigel@tendermint.com>
//
// and distributed under the terms of the Apache License, Version 2.0.
//
// You should have received a copy of the license along with this package;
// if not, you may find it at 'https://www.apache.org/licenses/LICENSE-2.0`

import (
	"bufio"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	yaml "gopkg.in/yaml.v2"
)

type Config struct {
	Sections map[string]string `yaml:"sections"`
	Tags     []string          `yaml:"tags"`
}

const (
	configFileName         = ".clog.yaml"
	entriesDirName         = ".pending"
	ghLinkPattern          = `#([0-9]+)`
	ghLinkExpanded         = `[\#$1](https://github.com/cosmos/cosmos-sdk/issues/$1)`
	maxEntryFilenameLength = 20
)

var (
	verboseLog *log.Logger
	config     Config
	cwd        string

	entriesDir      string
	verboseLogging  bool
	interactiveMode bool

	generateVerString string

	// rootCmd represents the base command when called without any subcommands
	rootCmd = &cobra.Command{
		Use:   "clog",
		Short: "Maintain unreleased changelog entries in a modular fashion.",
	}

	// command to add a pending log entry
	addCmd = &cobra.Command{
		Use:    "add [section] [tag] [message]",
		Short:  "Add an entry file.",
		Long:   `Add an entry file. If message is empty, start the editor to edit the message.`,
		Args:   cobra.MaximumNArgs(3),
		PreRun: loadConfig,
		RunE: func(cmd *cobra.Command, args []string) error {

			if interactiveMode {
				return addEntryFileFromConsoleInput()
			}

			if len(args) < 2 {
				log.Println("must include at least 2 arguments when not in interactive mode")
				return nil
			}
			sectionDir, tagDir := args[0], args[1]
			err := validateSectionTagDirs(sectionDir, tagDir)
			if err != nil {
				return err
			}
			if len(args) == 3 {
				return addSinglelineEntryFile(sectionDir, tagDir, strings.TrimSpace(args[2]))
			}
			return addEntryFile(sectionDir, tagDir)
		},
	}

	// command to generate the changelog
	generateCmd = &cobra.Command{
		Use:   "generate",
		Short: "Generate a changelog in Markdown format and print it to STDOUT.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return generateChangelog(generateVerString)
		},
		PreRun: loadConfig,
	}

	// command to delete empty sub-directories recursively
	pruneCmd = &cobra.Command{
		Use:   "prune",
		Short: "Delete empty sub-directories recursively.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return pruneEmptyDirectories()
		},
		PreRun: loadConfig,
	}
)

func loadConfig(_ *cobra.Command, _ []string) {
	var err error
	config, err = findAndReadConfig(cwd)
	if err != nil {
		log.Fatal(err)
	}
}

func init() {
	rootCmd.AddCommand(addCmd)
	rootCmd.AddCommand(generateCmd)
	rootCmd.AddCommand(pruneCmd)
	generateCmd.Flags().StringVarP(&generateVerString, "release", "r", "UNRELEASED", "release's version string")

	cwd = checkGetcwd()
	addCmd.Flags().BoolVarP(&interactiveMode, "interactive", "i", false, "get the section, tag, and message with interactive CLI prompts")
	rootCmd.PersistentFlags().BoolVarP(&verboseLogging, "verbose-logging", "v", false, "enable verbose logging")
	rootCmd.PersistentFlags().StringVarP(&entriesDir, "entries-dir", "d", filepath.Join(cwd, entriesDirName), "entry files directory")
}

func main() {
	logPrefix := fmt.Sprintf("%s: ", filepath.Base(os.Args[0]))
	log.SetFlags(0)
	log.SetPrefix(logPrefix)
	verboseLog = log.New(ioutil.Discard, "", 0)
	if verboseLogging {
		verboseLog.SetOutput(os.Stderr)
		verboseLog.SetPrefix(logPrefix)
	}

	if err := rootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
}

func addEntryFileFromConsoleInput() error {
	reader := bufio.NewReader(os.Stdin)
	fmt.Fprintf(os.Stderr, "Please enter the section %v: ", config.Sections)
	sectionDir, _ := reader.ReadString('\n')
	sectionDir = strings.TrimSpace(sectionDir)
	if _, ok := config.Sections[sectionDir]; !ok {
		return errors.New("invalid section, please try again")
	}

	fmt.Fprintf(os.Stderr, "Please enter the tag %v: ", config.Tags)
	tag, _ := reader.ReadString('\n')
	tag = strings.TrimSpace(tag)
	ok := false
	for _, t := range config.Tags {
		if t == tag {
			ok = true
			break
		}
	}
	if !ok {
		return errors.New("invalid tag, please try again")
	}

	fmt.Fprintf(os.Stderr, "Please enter the changelog message (or press enter to write in default $EDITOR)")
	message, _ := reader.ReadString('\n')
	message = strings.TrimSpace(message)
	if message == "" {
		return addEntryFile(sectionDir, tag)
	}

	return addSinglelineEntryFile(sectionDir, tag, message)
}

func addSinglelineEntryFile(sectionDir, tagDir, message string) error {
	filename := filepath.Join(
		filepath.Join(entriesDir, sectionDir, tagDir),
		generateFileName(message),
	)

	return writeEntryFile(filename, []byte(message))
}

func addEntryFile(sectionDir, tagDir string) error {
	bs, err := readUserInputFromEditor()
	if err != nil {
		return err
	}
	firstLine := strings.TrimSpace(strings.Split(string(bs), "\n")[0])
	filename := filepath.Join(
		filepath.Join(entriesDir, sectionDir, tagDir),
		generateFileName(firstLine),
	)

	return writeEntryFile(filename, bs)
}

func generateFileName(line string) string {
	var chunks []string

	filenameInvalidChars := regexp.MustCompile(`[^a-zA-Z0-9-_]`)
	subsWithInvalidCharsRemoved := strings.Split(filenameInvalidChars.ReplaceAllString(line, " "), " ")
	for _, sub := range subsWithInvalidCharsRemoved {
		sub = strings.TrimSpace(sub)
		if len(sub) != 0 {
			chunks = append(chunks, sub)
		}
	}

	ret := strings.Join(chunks, "-")

	if len(ret) > maxEntryFilenameLength {
		return ret[:maxEntryFilenameLength]
	}
	return ret
}

func directoryContents(dirPath string) ([]os.FileInfo, error) {
	contents, err := ioutil.ReadDir(dirPath)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("couldn't read directory %s: %v", dirPath, err)
	}

	if len(contents) == 0 {
		return nil, nil
	}

	// Filter out hidden files
	newContents := contents[:0]
	for _, f := range contents {
		if f.Name()[0] != '.' { // skip hidden files
			newContents = append(newContents, f)
		}
	}
	for i := len(newContents); i < len(contents); i++ {
		contents[i] = nil
	}

	return newContents, nil
}

func generateChangelog(version string) error {
	fmt.Printf("# %s\n\n", version)
	for sectionDir, sectionTitle := range config.Sections {
		sectionTitlePrinted := false
		for _, tag := range config.Tags {
			path := filepath.Join(entriesDir, sectionDir, tag)
			files, err := directoryContents(path)
			if err != nil {
				return err
			}
			if len(files) == 0 {
				continue
			}

			if !sectionTitlePrinted {
				fmt.Printf("## %s\n\n", sectionTitle)
				sectionTitlePrinted = true
			}

			for _, f := range files {
				verboseLog.Println("processing", f.Name())
				filename := filepath.Join(path, f.Name())
				if err := indentAndPrintFile(tag, filename); err != nil {
					return err
				}
			}

			fmt.Println()
		}
	}
	return nil
}

func pruneEmptyDirectories() error {
	for sectionDir := range config.Sections {
		for _, tag := range config.Tags {
			err := mustPruneDirIfEmpty(filepath.Join(entriesDir, sectionDir, tag))
			if err != nil {
				return err
			}
		}
		return mustPruneDirIfEmpty(filepath.Join(entriesDir, sectionDir))
	}
	return nil
}

// nolint: errcheck
func indentAndPrintFile(tag, filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	firstLine := true
	ghLinkRe := regexp.MustCompile(ghLinkPattern)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if len(line) == 0 {
			continue
		}

		linkified := ghLinkRe.ReplaceAllString(line, ghLinkExpanded)
		if firstLine {
			fmt.Printf("* (%s) %s\n", tag, linkified)
			firstLine = false
			continue
		}

		fmt.Printf("  %s\n", linkified)
	}

	return scanner.Err()
}

// nolint: errcheck
func writeEntryFile(filename string, bs []byte) error {
	if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
		return err
	}
	outFile, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	defer outFile.Close()

	if _, err := outFile.Write(bs); err != nil {
		return err
	}

	log.Printf("Unreleased changelog entry written to: %s\n", filename)
	log.Println("To modify this entry please edit or delete the above file directly.")
	return nil
}

func validateSectionTagDirs(sectionDir, tag string) error {
	if _, ok := config.Sections[sectionDir]; !ok {
		return fmt.Errorf("invalid section -- %s", sectionDir)
	}
	for _, t := range config.Tags {
		if t == tag {
			return nil
		}
	}
	return fmt.Errorf("invalid tag -- %s", tag)
}

// nolint: errcheck
func readUserInputFromEditor() ([]byte, error) {
	tempfilename, err := launchUserEditor()
	if err != nil {
		return []byte{}, fmt.Errorf("couldn't open an editor: %v", err)
	}
	defer os.Remove(tempfilename)
	bs, err := ioutil.ReadFile(tempfilename)
	if err != nil {
		return []byte{}, fmt.Errorf("error: %v", err)
	}
	return bs, nil
}

// nolint: errcheck
func launchUserEditor() (string, error) {
	editor, err := exec.LookPath("editor")
	if err != nil {
		editor = ""
	}
	if editor == "" {
		editor = os.Getenv("VISUAL")
	}
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		return "", errors.New("no editor set, make sure that either " +
			"VISUAL or EDITOR variables is set and pointing to a correct editor")
	}

	tempfile, err := ioutil.TempFile("", "clog_*")
	tempfilename := tempfile.Name()
	if err != nil {
		return "", err
	}
	tempfile.Close()

	cmd := exec.Command(editor, tempfilename)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	if err := cmd.Run(); err != nil {
		os.Remove(tempfilename)
		return "", err
	}

	fileInfo, err := os.Stat(tempfilename)
	if err != nil {
		os.Remove(tempfilename)
		return "", err
	}
	if fileInfo.Size() == 0 {
		return "", errors.New("aborting due to empty message")
	}

	return tempfilename, nil
}

func mustPruneDirIfEmpty(path string) error {
	contents, err := directoryContents(path)
	if err != nil {
		return err
	}
	if len(contents) != 0 {
		return nil
	}
	if err := os.Remove(path); err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	log.Println(path, "removed")
	return nil
}

func checkGetcwd() string {
	cwd, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	return cwd
}

func findAndReadConfig(dir string) (Config, error) {
	configFile, err := findConfigFile(dir)
	if err != nil {
		return Config{}, err
	}
	conf, err := readConfig(configFile)
	if err != nil {
		return Config{}, err
	}
	return conf, nil
}

func readConfig(configFile string) (Config, error) {
	var conf Config
	bs, err := ioutil.ReadFile(configFile)
	if err != nil {
		return conf, err
	}

	err = yaml.Unmarshal(bs, &conf)
	return conf, err
}

func findConfigFile(rootDir string) (string, error) {
	for {
		files, err := ioutil.ReadDir(rootDir)
		if err != nil {
			return "", err
		}
		for _, fp := range files {
			if fp.Name() == configFileName {
				return filepath.Join(rootDir, fp.Name()), nil
			}
		}
		parent := filepath.Dir(rootDir)
		if parent == rootDir {
			return "", errors.New("couldn't find configuration file")
		}
		rootDir = parent
	}
}
