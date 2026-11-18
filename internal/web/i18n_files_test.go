package web

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// Every area has i18n_vi_<area>.go and i18n_en_<area>.go with the same keys in the same order, and nothing else adds text.
func TestLanguageFilesArePaired(t *testing.T) {
	files, _ := filepath.Glob("i18n_*.go")
	keys := map[string][]string{}
	fset := token.NewFileSet()
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		lang, area, ok := strings.Cut(strings.TrimSuffix(strings.TrimPrefix(f, "i18n_"), ".go"), "_")
		if !ok || !supportedLanguage(lang) {
			t.Errorf("%s: name must be i18n_<language>_<area>.go", f)
			continue
		}
		af, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		var ks []string
		ast.Inspect(af, func(n ast.Node) bool {
			if r, ok := n.(*ast.RangeStmt); ok {
				if as, ok := r.Body.List[0].(*ast.AssignStmt); !ok || as.Lhs[0].(*ast.IndexExpr).X.(*ast.Ident).Name != lang {
					t.Errorf("%s must add to %s only", f, lang)
				}
				for _, e := range r.X.(*ast.CompositeLit).Elts {
					k, _ := strconv.Unquote(e.(*ast.KeyValueExpr).Key.(*ast.BasicLit).Value)
					ks = append(ks, k)
				}
			}
			return true
		})
		keys[lang+" "+area] = ks
	}
	for name, ks := range keys {
		lang, area, _ := strings.Cut(name, " ")
		for _, l := range languages {
			pair, ok := keys[l.Code+" "+area]
			if !ok {
				t.Errorf("i18n_%s_%s.go has no i18n_%s_%s.go", lang, area, l.Code, area)
			} else if !slices.Equal(ks, pair) {
				t.Errorf("i18n_%s_%s.go and i18n_%s_%s.go hold different keys", lang, area, l.Code, area)
			}
		}
	}
}
