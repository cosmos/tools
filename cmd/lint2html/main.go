package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	"os"
	"time"
)

type Pos struct {
	Filename string
	Offset   int
	Line     int
	Column   int
}

type Replacement struct {
	NeedOnlyDelete bool
}

type Issue struct {
	FromLinter  string
	Text        string
	SourceLines []string
	Replacement *Replacement
	Pos         *Pos
}

type Data struct {
	Issues []Issue
}

const htmlTemplate = `
<!DOCTYPE html>
<html>
	<head>
		<meta charset="UTF-8">
		<title>GolangCI report</title>
	</head>
	<style>
table, th, td {
  border: 1px solid black;
  border-collapse: collapse;
}
th, td {
	padding: 5px;
}
table {
	border-spacing: 15px;
}
</style>
	<body>
		<h2>Linter warnings for {{.Repository}} ({{.Branch}})</h2>
		<table style="width:100%" bgcolor="gainsboro">
		<tr><th>Filename</th><th>Linter</th><th>Warning</th></tr>
		{{ range .Issues }}
		<tr><td><a href="{{fileLineURL .Pos}}">{{fileLineText .Pos}}</a></td><td>{{ .FromLinter }}</td><td>{{ .Text }}</td></tr>
		{{end}}
		</table>
	<p><i>Page generated at {{ .Time }}</i></p>
	</body>
</html>`

func main() {
	repo := os.Args[1]
	branch := os.Args[2]

	if len(os.Args) != 3 {
		log.Fatal("lint2html PACKAGE BRANCH")
	}

	funcMap := template.FuncMap{
		"fileLineURL": func(p *Pos) string {
			return fmt.Sprintf(`https://%s/blob/%s/%s#L%d`, repo, branch, p.Filename, p.Line)
		},
		"fileLineText": func(p *Pos) string {
			return fmt.Sprintf(`%s:%d`, p.Filename, p.Line)
		},
	}
	t := template.Must(template.New("t").Funcs(funcMap).Parse(htmlTemplate))

	check := func(err error) {
		if err != nil {
			log.Fatal(err)
		}
	}

	raw, err := ioutil.ReadAll(os.Stdin)
	check(err)
	var data Data
	check(json.Unmarshal(raw, &data))

	check(t.Execute(os.Stdout, container{
		Repository: repo,
		Branch:     branch,
		Issues:     data.Issues,
		Time:       time.Now().UTC(),
	}))
}

type container struct {
	Repository string
	Branch     string
	Issues     []Issue
	Time       time.Time
}
