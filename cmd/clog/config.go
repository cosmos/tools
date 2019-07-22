package main

import (
	"errors"
	"io/ioutil"
	"log"
	"path/filepath"

	"github.com/spf13/cobra"
	yaml "gopkg.in/yaml.v2"
)

type Config struct {
	Sections map[string]string `yaml:"sections"`
	Tags     []string          `yaml:"tags"`
}

func (cfg Config) sections() []string {
	sections := make([]string, len(cfg.Sections))

	var i int
	for s := range cfg.Sections {
		sections[i] = s
		i++
	}

	return sections
}

func loadConfig(_ *cobra.Command, _ []string) {
	var err error

	config, err = findAndReadConfig(cwd)
	if err != nil {
		log.Fatal(err)
	}
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
