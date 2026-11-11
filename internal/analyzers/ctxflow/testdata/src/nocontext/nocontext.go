package nocontext

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"net/http"
	"os/exec"
	"strings"
)

func useCtx(...any) {}

type otherDeps struct {
	tr   *http.Transport
	stmt *sql.Stmt
	sb   *strings.Builder
}

func nocontextReported(ctx context.Context, db *sql.DB, tx *sql.Tx, client *http.Client) {
	net.Dial("tcp", "x:80")                                  // want "net\\.Dial cannot be cancelled"
	http.Get("http://example.com")                           // want "http\\.Get cannot be cancelled"
	http.Head("http://example.com")                          // want "http\\.Head cannot be cancelled"
	http.Post("http://example.com", "text/plain", nil)       // want "http\\.Post cannot be cancelled"
	http.PostForm("http://example.com", nil)                 // want "http\\.PostForm cannot be cancelled"
	client.Get("http://example.com")                         // want "Client\\)\\.Get cannot be cancelled"
	client.Head("http://example.com")                        // want "Client\\)\\.Head cannot be cancelled"
	client.Post("http://example.com", "text/plain", nil)     // want "Client\\)\\.Post cannot be cancelled"
	client.PostForm("http://example.com", nil)               // want "Client\\)\\.PostForm cannot be cancelled"
	req, _ := http.NewRequest("GET", "http://example.com", nil) // want "http\\.NewRequest cannot be cancelled"
	_, _ = client.Do(req)
	cmd := exec.Command("ls") // want "exec\\.Command cannot be cancelled"
	_ = cmd.Run()
	db.Query("SELECT 1")                                     // want "DB\\)\\.Query cannot be cancelled"
	db.QueryRow("SELECT 1")                                  // want "DB\\)\\.QueryRow cannot be cancelled"
	db.Exec("SELECT 1")                                      // want "DB\\)\\.Exec cannot be cancelled"
	_, _ = db.Prepare("SELECT 1")                            // want "DB\\)\\.Prepare cannot be cancelled"
	tx.Query("SELECT 1")                                     // want "Tx\\)\\.Query cannot be cancelled"
	tx.QueryRow("SELECT 1")                                  // want "Tx\\)\\.QueryRow cannot be cancelled"
	tx.Exec("SELECT 1")                                      // want "Tx\\)\\.Exec cannot be cancelled"
	_, _ = tx.Prepare("SELECT 1")                            // want "Tx\\)\\.Prepare cannot be cancelled"
	db.Begin()                                               // want "DB\\)\\.Begin cannot be cancelled"
	db.Ping()                                                // want "DB\\)\\.Ping cannot be cancelled"
}

type ctxAlias = context.Context

func nocontextReportedAlias(ctx ctxAlias, db *sql.DB) {
	db.Query("SELECT 1") // want "DB\\)\\.Query cannot be cancelled"
}

func nocontextSilent(db *sql.DB) {
	db.Query("SELECT 1")
	net.Dial("tcp", "x:80")
}

func nocontextSilentCtx(ctx context.Context, db *sql.DB) {
	_, _ = db.QueryContext(ctx, "SELECT 1")
	exec.CommandContext(ctx, "ls")
}

func nocontextSilentOther(ctx context.Context, client *http.Client, db *sql.DB, tx *sql.Tx, d otherDeps) {
	useCtx(ctx)
	_ = net.JoinHostPort("a", "80")
	http.Error(nil, "x", 500)
	_, _ = exec.LookPath("ls")
	_ = d.tr.Clone()
	_ = db.Close()
	_ = tx.Commit
	_, _ = d.stmt.QueryContext(ctx, 1)
	d.sb.WriteString("x")
	fn := func() {}
	fn()
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://example.com", nil)
	_, _ = client.Do(req)
}

func nocontextSilentBlankParam(_ context.Context) {
	net.Dial("tcp", "x:80")
}

func nocontextSilentUnnamedParam(context.Context) {
	net.Dial("tcp", "x:80")
}

func nocontextSilentLocalAfter() {
	net.Dial("tcp", "x:80")
	ctx := context.Background()
	useCtx(ctx)
}

func nocontextReportedLocalCtx() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	net.Dial("tcp", "x:80") // want "net\\.Dial cannot be cancelled"
	useCtx(ctx)
}

func nocontextSilentListen(ctx context.Context) {
	lc := &net.ListenConfig{}
	ln, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return
	}
	go func() { <-ctx.Done(); ln.Close() }()
	useCtx(ctx)
}

func nocontextSilentNewRequestWithContext(ctx context.Context, url string) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return
	}
	_, _ = http.DefaultClient.Do(req.WithContext(ctx))
}

func doWithCtx(ctx context.Context, req *http.Request) error {
	_, err := http.DefaultClient.Do(req.WithContext(ctx))
	return err
}

func nocontextSilentNewRequestPassedWithContext(ctx context.Context, url string) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	return doWithCtx(ctx, req)
}

func nocontextReportedNewRequestBackgroundArg(ctx context.Context, u string) error {
	req, err := http.NewRequest(http.MethodGet, u, nil) // want "http\\.NewRequest cannot be cancelled"
	if err != nil {
		return err
	}
	_, err = http.DefaultClient.Do(req.WithContext(context.Background()))
	return err
}

func nocontextReportedNewRequestBareWithContext(ctx context.Context, u string) error {
	req, err := http.NewRequest(http.MethodGet, u, nil) // want "http\\.NewRequest cannot be cancelled"
	if err != nil {
		return err
	}
	req.WithContext(ctx)
	_, err = http.DefaultClient.Do(req)
	return err
}

var nocontextBackground = context.Background()

func nocontextSilentPackageVar(db *sql.DB) error {
	_ = nocontextBackground
	_, err := db.Query("SELECT 1")
	return err
}

type nocontextHolder struct {
	base context.Context
}

func nocontextSilentStructField(s nocontextHolder, db *sql.DB) error {
	_ = s.base
	_, err := db.Query("SELECT 1")
	return err
}

func nocontextSilentScopedLocal(db *sql.DB, ok bool) error {
	if ok {
		ctx := context.Background()
		_ = ctx
	}
	_, err := db.Query("SELECT 1")
	return err
}

type nocontextDialerSilent struct{}

type nocontextDialerReported struct{}

func (nocontextDialerSilent) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	type dialResult struct {
		conn net.Conn
		err  error
	}
	ch := make(chan dialResult, 1)
	go func() {
		conn, err := net.Dial(network, address)
		ch <- dialResult{conn, err}
	}()
	select {
	case <-ctx.Done():
		go func() {
			r := <-ch
			if r.err == nil {
				r.conn.Close()
			}
		}()
		return nil, ctx.Err()
	case r := <-ch:
		return r.conn, r.err
	}
}

func (nocontextDialerReported) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	_ = ctx
	return net.Dial(network, address) // want "net\\.Dial cannot be cancelled"
}

func (nocontextDialerNilCompare) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	return net.Dial(network, address) // want "net\\.Dial cannot be cancelled"
}

func (nocontextDialerReassigned) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	ctx = context.Background()
	return net.Dial(network, address) // want "net\\.Dial cannot be cancelled"
}

func (nocontextDialerBlankDef) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	var _ = ctx
	return net.Dial(network, address) // want "net\\.Dial cannot be cancelled"
}

type nocontextDialerNilCompare struct{}

type nocontextDialerReassigned struct{}

type nocontextDialerBlankDef struct{}

type nocontextDialerSilentAlias struct{}

type nocontextDialState struct {
	ctx context.Context
}

type nocontextDialerSilentStruct struct{}

func (nocontextDialerSilentAlias) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	type dialResult struct {
		conn net.Conn
		err  error
	}
	c := ctx
	ch := make(chan dialResult, 1)
	go func() {
		conn, err := net.Dial(network, address)
		ch <- dialResult{conn, err}
	}()
	select {
	case <-c.Done():
		go func() {
			r := <-ch
			if r.err == nil {
				r.conn.Close()
			}
		}()
		return nil, c.Err()
	case r := <-ch:
		return r.conn, r.err
	}
}

func (nocontextDialerSilentStruct) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	type dialResult struct {
		conn net.Conn
		err  error
	}
	st := nocontextDialState{ctx: ctx}
	ch := make(chan dialResult, 1)
	go func() {
		conn, err := net.Dial(network, address)
		ch <- dialResult{conn, err}
	}()
	select {
	case <-st.ctx.Done():
		go func() {
			r := <-ch
			if r.err == nil {
				r.conn.Close()
			}
		}()
		return nil, st.ctx.Err()
	case r := <-ch:
		return r.conn, r.err
	}
}

type nocontextSender struct {
	req *http.Request
	url string
}

func (s *nocontextSender) nocontextSilentFieldWithContext(ctx context.Context) error {
	var err error
	s.req, err = http.NewRequest(http.MethodGet, s.url, nil)
	if err != nil {
		return err
	}
	s.req = s.req.WithContext(ctx)
	return nil
}

func nocontextSilentManualCancel(ctx context.Context) error {
	cmd := exec.Command("sleep", "10")
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-ctx.Done():
		_ = cmd.Process.Kill()
		<-done
		return ctx.Err()
	case err := <-done:
		return err
	}
}

func nocontextReportedDoneWithoutTouch(ctx context.Context) error {
	cmd := exec.Command("sleep", "10") // want "exec\\.Command cannot be cancelled"
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		return err
	}
}
