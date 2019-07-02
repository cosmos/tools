[![Build Status](https://travis-ci.com/alessio/clog.svg?branch=master)](https://travis-ci.com/alessio/clog)
[![GolangCI](https://golangci.com/badges/github.com/alessio/clog.svg)](https://golangci.com/r/github.com/alessio/clog)

# clog

Maintain changelog files in modular fashion 

# Installation

```
$ go get -u github.com/alessio/clog
```

# Usage

```
$ clog help
Maintain unreleased changelog entries in a modular fashion.

Usage:
  clog [command]

Available Commands:
  add         Add an entry file.
  generate    Generate a changelog in Markdown format and print it to STDOUT.
  help        Help about any command
  prune       Delete empty sub-directories recursively.

Flags:
  -d, --entries-dir string   entry files directory (default "$CWD/.pending")
  -h, --help                 help for clog
  -v, --verbose-logging      enable verbose logging

Use "clog [command] --help" for more information about a command.
```

## Add a new entry

You can either drop a text file in the appropriate directory or use the `add` command:

```bash
$ clog add features gaiacli '#3452 New cool gaiacli command'
```

If no message is provided, a new entry file is opened in an editor is started

## Generate the full changelog

```bash
$ clog generate v0.30.0
```

The `-prune` flag would remove the old entry files after the changelog is generated.
