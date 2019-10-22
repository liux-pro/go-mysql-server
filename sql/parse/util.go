package parse

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"strings"
	"unicode"

	"github.com/src-d/go-mysql-server/sql"
	errors "gopkg.in/src-d/go-errors.v1"
	"vitess.io/vitess/go/vt/sqlparser"
)

var (
	errUnexpectedSyntax       = errors.NewKind("expecting %q but got %q instead")
	errInvalidIndexExpression = errors.NewKind("invalid expression to index: %s")
)

type parseFunc func(*bufio.Reader) error

type parseFuncs []parseFunc

func (f parseFuncs) exec(r *bufio.Reader) error {
	for _, fn := range f {
		if err := fn(r); err != nil {
			return err
		}
	}
	return nil
}

func expectRune(expected rune) parseFunc {
	return func(rd *bufio.Reader) error {
		r, _, err := rd.ReadRune()
		if err != nil {
			return err
		}

		if r != expected {
			return errUnexpectedSyntax.New(expected, string(r))
		}

		return nil
	}
}

func expect(expected string) parseFunc {
	return func(r *bufio.Reader) error {
		var ident string

		if err := readIdent(&ident)(r); err != nil {
			return err
		}

		if ident == expected {
			return nil
		}

		return errUnexpectedSyntax.New(expected, ident)
	}
}

func skipSpaces(r *bufio.Reader) error {
	for {
		ru, _, err := r.ReadRune()
		if err == io.EOF {
			return nil
		}

		if err != nil {
			return err
		}

		if !unicode.IsSpace(ru) {
			return r.UnreadRune()
		}
	}
}

func checkEOF(rd *bufio.Reader) error {
	r, _, err := rd.ReadRune()
	if err == io.EOF {
		return nil
	}

	return errUnexpectedSyntax.New("EOF", r)
}

func optional(steps ...parseFunc) parseFunc {
	return func(rd *bufio.Reader) error {
		for _, step := range steps {
			err := step(rd)
			if err == io.EOF || errUnexpectedSyntax.Is(err) {
				return nil
			}

			if err != nil {
				return err
			}
		}

		return nil
	}
}

func readLetter(r *bufio.Reader, buf *bytes.Buffer) error {
	ru, _, err := r.ReadRune()
	if err != nil {
		if err == io.EOF {
			return nil
		}

		return err
	}

	if !unicode.IsLetter(ru) {
		if err := r.UnreadRune(); err != nil {
			return err
		}
		return nil
	}

	buf.WriteRune(ru)
	return nil
}

func readLetterOrPoint(r *bufio.Reader, buf *bytes.Buffer) error {
	ru, _, err := r.ReadRune()
	if err != nil {
		if err == io.EOF {
			return nil
		}

		return err
	}

	if !unicode.IsLetter(ru) && ru != '.' {
		if err := r.UnreadRune(); err != nil {
			return err
		}
		return nil
	}

	buf.WriteRune(ru)
	return nil
}

func readValidIdentRune(r *bufio.Reader, buf *bytes.Buffer) error {
	ru, _, err := r.ReadRune()
	if err != nil {
		return err
	}

	if !unicode.IsLetter(ru) && !unicode.IsDigit(ru) && ru != '_' {
		if err := r.UnreadRune(); err != nil {
			return err
		}
		return io.EOF
	}

	buf.WriteRune(ru)
	return nil
}

func readValidScopedIdentRune(r *bufio.Reader, separator rune, buf *bytes.Buffer) error {
	ru, _, err := r.ReadRune()
	if err != nil {
		return err
	}

	if !unicode.IsLetter(ru) && !unicode.IsDigit(ru) && ru != '_' && ru != separator {
		if err := r.UnreadRune(); err != nil {
			return err
		}
		return io.EOF
	}

	buf.WriteRune(ru)
	return nil
}

func readValidQuotedIdentRune(r *bufio.Reader, buf *bytes.Buffer) error {
	bs, err := r.Peek(2)
	if err != nil {
		return err
	}

	if bs[0] == '`' && bs[1] == '`' {
		if _, _, err := r.ReadRune(); err != nil {
			return err
		}
		if _, _, err := r.ReadRune(); err != nil {
			return err
		}
		buf.WriteRune('`')
		return nil
	}

	if bs[0] == '`' && bs[1] != '`' {
		return io.EOF
	}

	if _, _, err := r.ReadRune(); err != nil {
		return err
	}

	buf.WriteByte(bs[0])

	return nil
}

func unreadString(r *bufio.Reader, str string) {
	nr := *r
	r.Reset(io.MultiReader(strings.NewReader(str), &nr))
}

func readIdent(ident *string) parseFunc {
	return func(r *bufio.Reader) error {
		var buf bytes.Buffer
		if err := readLetter(r, &buf); err != nil {
			return err
		}

		for {
			if err := readValidIdentRune(r, &buf); err == io.EOF {
				break
			} else if err != nil {
				return err
			}
		}

		*ident = strings.ToLower(buf.String())
		return nil
	}
}

func readScopedIdent(separator rune, idents *[]string) parseFunc {
	return func(r *bufio.Reader) error {
		var buf bytes.Buffer
		if err := readLetter(r, &buf); err != nil {
			return err
		}

		for {
			if err := readValidScopedIdentRune(r, separator, &buf); err == io.EOF {
				break
			} else if err != nil {
				return err
			}
		}

		*idents = append(
			*idents,
			strings.Split(strings.ToLower(buf.String()), string(separator))...,
		)
		return nil
	}
}

func readQuotedIdent(ident *string) parseFunc {
	return func(r *bufio.Reader) error {
		var buf bytes.Buffer
		if err := readValidQuotedIdentRune(r, &buf); err != nil {
			return err
		}

		for {
			if err := readValidQuotedIdentRune(r, &buf); err == io.EOF {
				break
			} else if err != nil {
				return err
			}
		}

		*ident = strings.ToLower(buf.String())
		return nil
	}
}

func oneOf(options ...string) parseFunc {
	return func(r *bufio.Reader) error {
		var ident string
		if err := readIdent(&ident)(r); err != nil {
			return err
		}

		for _, opt := range options {
			if strings.ToLower(opt) == ident {
				return nil
			}
		}

		return errUnexpectedSyntax.New(
			fmt.Sprintf("one of: %s", strings.Join(options, ", ")),
			ident,
		)
	}
}

func readRemaining(val *string) parseFunc {
	return func(r *bufio.Reader) error {
		bytes, err := ioutil.ReadAll(r)
		if err != nil {
			return err
		}

		*val = string(bytes)
		return nil
	}
}

func parseExpr(ctx *sql.Context, str string) (sql.Expression, error) {
	stmt, err := sqlparser.Parse("SELECT " + str)
	if err != nil {
		return nil, err
	}

	selectStmt, ok := stmt.(*sqlparser.Select)
	if !ok {
		return nil, errInvalidIndexExpression.New(str)
	}

	if len(selectStmt.SelectExprs) != 1 {
		return nil, errInvalidIndexExpression.New(str)
	}

	selectExpr, ok := selectStmt.SelectExprs[0].(*sqlparser.AliasedExpr)
	if !ok {
		return nil, errInvalidIndexExpression.New(str)
	}

	return exprToExpression(ctx, selectExpr.Expr)
}

func readQuotableIdent(ident *string) parseFunc {
	return func(r *bufio.Reader) error {
		nextChar, err := r.Peek(1)
		if err != nil {
			return err
		}

		var steps parseFuncs
		if nextChar[0] == '`' {
			steps = parseFuncs{
				expectQuote,
				readQuotedIdent(ident),
				expectQuote,
			}
		} else {
			steps = parseFuncs{readIdent(ident)}
		}

		return steps.exec(r)
	}
}

func expectQuote(r *bufio.Reader) error {
	ru, _, err := r.ReadRune()
	if err != nil {
		return err
	}

	if ru != '`' {
		return errUnexpectedSyntax.New("`", string(ru))
	}

	return nil
}

func maybe(matched *bool, str string) parseFunc {
	return func(rd *bufio.Reader) error {
		*matched = false
		strLength := len(str)

		data, err := rd.Peek(strLength)
		if err != nil {
			// If there are not enough runes, what we expected was not there, which
			// is not an error per se.
			if len(data) < strLength {
				return nil
			}

			return err
		}

		if strings.ToLower(string(data)) == str {
			_, err := rd.Discard(strLength)
			if err != nil {
				return err
			}

			*matched = true
			return nil
		}

		return nil
	}
}

func multiMaybe(matched *bool, strings ...string) parseFunc {
	return func(rd *bufio.Reader) error {
		*matched = false
		first := true
		for _, str := range strings {
			if err := maybe(matched, str)(rd); err != nil {
				return err
			}

			if !*matched {
				if first {
					return nil
				}

				// TODO: add actual string parsed
				return errUnexpectedSyntax.New(str, "smth else")
			}

			first = false

			if err := skipSpaces(rd); err != nil {
				return err
			}
		}
		*matched = true
		return nil
	}
}

// Read a list of strings separated by the specified separator, with a rune
// indicating the opening of the list and another one specifying its closing.
// For example, readList('(', ',', ')', list) parses "(uno,  dos,tres)" and
// populates list with the array of strings ["uno", "dos", "tres"]
// If the opening is not found, do not advance the reader
func maybeList(opening, separator, closing rune, list *[]string) parseFunc {
	return func(rd *bufio.Reader) error {
		r, _, err := rd.ReadRune()
		if err != nil {
			return err
		}

		if r != opening {
			rd.UnreadRune()
			return nil
		}

		for {
			var newItem string
			err := parseFuncs{
				skipSpaces,
				readIdent(&newItem),
				skipSpaces,
			}.exec(rd)

			if err != nil {
				return err
			}

			r, _, err := rd.ReadRune()
			if err != nil {
				return err
			}

			switch r {
			case closing:
				*list = append(*list, newItem)
				return nil
			case separator:
				*list = append(*list, newItem)
				continue
			default:
				return errUnexpectedSyntax.New(
					fmt.Sprintf("%v or %v", separator, closing),
					string(r),
				)
			}
		}
	}
}
