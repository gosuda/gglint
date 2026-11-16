package driver

import (
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"regexp"
	"strings"

	"github.com/gosuda/gglint/internal/finding"
)

var (
	typecheckLine     = regexp.MustCompile(`^(?:vet: )?(.+\.go):(\d+)(?::(\d+))?: (.+)$`)
	packageHeader     = regexp.MustCompile(`^# [^[:space:]:]+(?: \[[^[:space:]:]+\.test\])?$`)
	moduleDownload    = regexp.MustCompile(`^go: downloading [^[:space:]]+ v[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.+-]+)?$`)
	toolchainDownload = regexp.MustCompile(`^go: downloading go[0-9][0-9A-Za-z.]* \([a-z0-9]+/[a-z0-9]+\)$`)
	packageAnnotation = regexp.MustCompile(`^/\* package [^[:space:]:*]+ \*/$`)
)

// parseStderr maps source positions as before and reports whether the complete
// stderr is explained by source diagnostics. An unknown line must not disappear
// beside a type error when the command failed.
func parseStderr(stderr []byte, workDir string) (findings []finding.Finding, sourceOnly bool) {
	known, attached := true, false
	for line := range strings.SplitSeq(string(stderr), "\n") {
		line = strings.TrimRight(line, "\r")
		m := typecheckLine.FindStringSubmatch(line)
		if m == nil {
			if attached && typecheckContinuation(line) {
				continue
			}
			attached = false
			if !diagnosticMetadata(line) {
				known = false
			}
			continue
		}
		attached = true
		if toolFailureMessage(m[4]) {
			known = false
		}
		findings = append(findings, finding.Finding{
			Rule:    finding.TypecheckRule,
			Message: m[4],
			Pos:     finding.Position{Path: absolute(m[1], workDir), Line: number(m[2]), Column: number(m[3])},
		})
	}
	return findings, known && len(findings) != 0
}

func diagnosticMetadata(line string) bool {
	return strings.TrimSpace(line) == "" || packageHeader.MatchString(line) ||
		moduleDownload.MatchString(line) || toolchainDownload.MatchString(line)
}

func toolFailureMessage(message string) bool {
	message = strings.TrimSpace(message)
	return strings.HasPrefix(message, "internal compiler error:") ||
		strings.HasPrefix(message, "panic:") ||
		strings.HasPrefix(message, "fatal error:") ||
		message == "signal: killed" || strings.HasSuffix(message, ": signal: killed")
}

// Go type errors print argument tuples or method signatures after have/want.
// Indentation or those words alone do not establish a diagnostic continuation.
func typecheckContinuation(line string) bool {
	kind, signature, ok := splitDiagnosticContinuation(line)
	if !ok {
		return false
	}
	name, params, ok := strings.Cut(signature, "(")
	if !ok || name != "" && !token.IsIdentifier(name) {
		return false
	}
	// An expanded final argument is printed as T... in have tuples. The
	// element remains a type after removing that formatter-only suffix.
	if kind == "have" && name == "" && strings.HasSuffix(params, "...)") {
		params = strings.TrimSuffix(params, "...)") + ")"
	}
	return validDiagnosticSignature("func("+params, name != "")
}

func splitDiagnosticContinuation(line string) (kind, signature string, ok bool) {
	if line == "" || line[0] != '\t' && line[0] != ' ' {
		return "", "", false
	}
	kind, signature, ok = strings.Cut(strings.TrimLeft(line, " \t"), " ")
	return kind, signature, ok && (kind == "have" || kind == "want")
}

// validDiagnosticSignature requires the complete text to describe one function
// type. Only method signatures may have results after the parameter tuple.
func validDiagnosticSignature(source string, allowResults bool) bool {
	source, ok := normalizeDiagnosticTypes(source)
	if !ok {
		return false
	}
	fset := token.NewFileSet()
	expr, err := parser.ParseExprFrom(fset, "", source, 0)
	if err != nil {
		return false
	}
	sig, ok := expr.(*ast.FuncType)
	return ok && (allowResults || sig.Results == nil) &&
		strings.TrimSpace(source[fset.Position(sig.End()).Offset:]) == ""
}

// go/types uses quoted package qualifiers and pseudo-types in diagnostics.
// Normalize only those tokens so the Go parser can check the full signature.
// Preserve struct tags and the formatter's package annotations. Other comments
// are not diagnostic types and could hide a tool error from the parser.
func normalizeDiagnosticTypes(source string) (string, bool) {
	var scan scanner.Scanner
	file := token.NewFileSet().AddFile("", -1, len(source))
	scan.Init(file, []byte(source), nil, scanner.ScanComments)
	var out strings.Builder
	cursor := 0
	pos, tok, lit := scan.Scan()
	for tok != token.EOF {
		if tok == token.COMMENT && !packageAnnotation.MatchString(lit) {
			return "", false
		}
		nextPos, nextTok, nextLit := scan.Scan()
		start := file.Offset(pos)
		replacement, paired := diagnosticTypeReplacement(tok, lit, nextTok)
		if replacement != "" {
			out.WriteString(source[cursor:start])
			out.WriteString(replacement)
			cursor = start + len(lit)
			if paired {
				cursor = file.Offset(nextPos) + len("type")
				pos, tok, lit = scan.Scan()
				continue
			}
		}
		pos, tok, lit = nextPos, nextTok, nextLit
	}
	if scan.ErrorCount != 0 {
		return "", false
	}
	out.WriteString(source[cursor:])
	return out.String(), true
}

func diagnosticTypeReplacement(tok token.Token, lit string, next token.Token) (replacement string, paired bool) {
	if tok == token.STRING && strings.HasPrefix(lit, `"`) && next == token.PERIOD {
		return "gglintPackage", false
	}
	if tok == token.IDENT && (lit == "unknown" || lit == "invalid") && next == token.TYPE {
		return "gglintDiagnosticType", true
	}
	return "", false
}
