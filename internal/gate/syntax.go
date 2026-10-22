package gate

import (
	"errors"
	"go/parser"
	"go/scanner"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// syntaxFix is the one-sentence fix shown for every syntax item.
const syntaxFix = "Fix the syntax error; nothing else can be checked until the file parses."

// CheckSyntax parses each changed Go file and reports the first syntax error per file.
func CheckSyntax(root string, paths []string) []Item {
	items := []Item{}
	for _, p := range paths {
		if !strings.HasSuffix(p, ".go") || !filepath.IsLocal(filepath.FromSlash(p)) {
			continue
		}
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(p)))
		if err != nil {
			// A deleted or unreadable file has no syntax to check.
			continue
		}
		_, parseErr := parser.ParseFile(token.NewFileSet(), p, src, parser.AllErrors|parser.SkipObjectResolution)
		if parseErr != nil {
			items = append(items, syntaxItem(p, parseErr))
		}
	}
	return items
}

// syntaxItem converts a parser error into a blocking item at its first error.
func syntaxItem(file string, err error) Item {
	line, msg := 0, err.Error()
	var list scanner.ErrorList
	if errors.As(err, &list) && len(list) > 0 {
		line, msg = list[0].Pos.Line, list[0].Msg
	}
	return Item{
		Severity: SeverityBlock,
		Code:     CodeSyntax,
		File:     file,
		Line:     line,
		Message:  "syntax error: " + msg,
		Fix:      syntaxFix,
	}
}
