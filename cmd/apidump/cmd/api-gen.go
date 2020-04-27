package main

import (
	"fmt"
	"os"

	"github.com/cosmos/api-generator/parser"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "api-gen folder",
	Short: "Api gen generates an output showing the list of exported api calls in a folder",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		extractor, err := parser.NewPackageExtractor(args[0])
		if err != nil {
			return err
		}

		pkgs, err := extractor.Extract()
		if err != nil {
			return err
		}

		walker := parser.NewWalker(pkgs)
		pkgstypes, err := walker.Extract()
		if err != nil {
			return err
		}

		printer := parser.NewPrinter(pkgstypes, os.Stdout)
		printer.Print()

		return nil
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func main() {
	Execute()
}
