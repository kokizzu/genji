package doc_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/genjidb/genji/cmd/genji/doc"
	"github.com/genjidb/genji/internal/expr/functions"
	"github.com/genjidb/genji/internal/sql/scanner"
	"github.com/genjidb/genji/internal/testutil/assert"
	"github.com/stretchr/testify/require"
)

func TestFunctions(t *testing.T) {
	packages := functions.DefaultPackages()
	for pkgname, pkg := range packages {
		for fname, def := range pkg {
			if pkgname == "" {
				var isAlias = false
				for pkgname2, pkg2 := range packages {
					if pkgname2 != "" {
						_, ok := pkg2[strings.ToLower(fname)]
						if ok {
							isAlias = true
						}
					}
				}
				if !isAlias {
					t.Run(fmt.Sprintf("%s is documented and has all its arguments mentioned", fname), func(t *testing.T) {
						str, err := doc.DocString(fname)
						assert.NoError(t, err)
						for i := 0; i < def.Arity(); i++ {
							require.Contains(t, trimDocPromt(str), fmt.Sprintf("arg%d", i+1))
						}
					})
				}
			} else {
				t.Run(fmt.Sprintf("%s.%s is documented and has all its arguments mentioned", pkgname, fname), func(t *testing.T) {
					str, err := doc.DocString(fmt.Sprintf("%s.%s", pkgname, fname))
					assert.NoError(t, err)
					if def.Arity() > 0 {
						for i := 0; i < def.Arity(); i++ {
							require.Contains(t, trimDocPromt(str), fmt.Sprintf("arg%d", i+1))
						}
					}
				})
			}
		}
	}
}

// trimDocPrompt returns the description part of the doc string, ignoring the promt.
func trimDocPromt(str string) string {
	// Matches the doc description, ignoring the "package.funcname:" part.
	r := regexp.MustCompile("[^:]+:(.*)")
	subs := r.FindStringSubmatch(str)
	return subs[1]
}

func TestTokens(t *testing.T) {
	for _, tok := range scanner.AllKeywords() {
		t.Run(fmt.Sprintf("%s is documented", tok.String()), func(t *testing.T) {
			str, err := doc.DocString(tok.String())
			assert.NoError(t, err)
			require.NotEqual(t, "", str)
			if str == "TODO" {
				t.Logf("warning, %s is not yet documented", tok.String())
			} else {
				// if the token is documented, its description should contain its own name.
				require.Contains(t, str, tok.String())
			}
		})
	}
}

func TestDocString(t *testing.T) {
	t.Run("OK", func(t *testing.T) {
		str, err := doc.DocString("BY")
		assert.NoError(t, err)
		require.NotEmpty(t, str)
		require.NotEqual(t, "TODO", str)
	})

	t.Run("NOK illegal input", func(t *testing.T) {
		_, err := doc.DocString("😀")
		assert.ErrorIs(t, err, doc.ErrInvalid)
	})

	t.Run("NOK empty input", func(t *testing.T) {
		_, err := doc.DocString("")
		assert.ErrorIs(t, err, doc.ErrInvalid)
	})

	t.Run("NOK no doc found", func(t *testing.T) {
		_, err := doc.DocString("foo.bar")
		assert.ErrorIs(t, err, doc.ErrNotFound)
	})
}
