package errdrop

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"
	"text/template"

	"errdrop/dep"
)

type Config struct {
	Port int
}

type Tx struct{}

var errTx = errors.New("tx failed")

func (t *Tx) Commit() error {
	return errTx
}

func valueReported(p string) string {
	f, _ := os.Open(p) // want "is used after the error of os.Open is dropped"
	return f.Name()
}

func valueSilent(p string) string {
	f, _ := os.Open(p)
	if f == nil {
		return ""
	}
	return f.Name()
}

func valueSilent2(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func valueSilent3(s string) *url.URL {
	u, _ := url.Parse(s)
	return u
}

func outReported(b []byte) Config {
	var c Config
	json.Unmarshal(b, &c) // want "is read after the error of json.Unmarshal is dropped"
	return c
}

func outReported2(tmpl *template.Template, d string) string {
	var buf bytes.Buffer
	tmpl.Execute(&buf, d) // want "is read after the error of tmpl.Execute is dropped"
	return buf.String()
}

func outSilent() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "x")
	return sb.String()
}

func outSilent2(b []byte) Config {
	var c Config
	_ = json.Unmarshal(b, &c)
	return c
}

func outSilent3(b []byte) Config {
	c := Config{Port: 1}
	json.Unmarshal(b, &c)
	return c
}

func durableReported(p string, b []byte) error {
	f, err := os.Create(p)
	if err != nil {
		return err
	}
	defer f.Close() // want "is written in this function and the error of f.Close is dropped"
	_, err = f.Write(b)
	return err
}

func durableSilent(p string) error {
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	defer f.Close()
	b := make([]byte, 1)
	_, err = f.Read(b)
	return err
}

func durableSilent2(p string, b []byte) error {
	f, err := os.Create(p)
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	if err != nil {
		return err
	}
	return f.Close()
}

func durableSilent3(w io.WriteCloser) error {
	defer w.Close()
	return nil
}

func verdictReported(a, b string) error {
	os.Rename(a, b) // want "the error of os.Rename is dropped and the next statement returns nil"
	return nil
}

func verdictReported2(t *Tx) error {
	t.Commit() // want "the error of t.Commit is dropped and the next statement returns nil"
	return nil
}

func verdictSilent(h hash.Hash, b []byte) error {
	h.Write(b)
	return nil
}

func verdictSilent2(buf *bytes.Buffer, s string) error {
	buf.WriteString(s)
	return nil
}

func verdictSilent3(p string) error {
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	f.Close()
	return err
}

func verdictSilent4(p string) error {
	_ = os.Remove(p)
	return nil
}

func factNever() error {
	dep.Never()
	return nil
}

func factWrite() error {
	fmt.Fprintf(dep.T{}, "x")
	return nil
}
