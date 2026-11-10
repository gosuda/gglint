package nocontext

import (
	"context"
	"database/sql"
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
	net.Listen("tcp", ":0")                                  // want "net\\.Listen cannot be cancelled"
	http.Get("http://example.com")                           // want "http\\.Get cannot be cancelled"
	http.Head("http://example.com")                          // want "http\\.Head cannot be cancelled"
	http.Post("http://example.com", "text/plain", nil)       // want "http\\.Post cannot be cancelled"
	http.PostForm("http://example.com", nil)                 // want "http\\.PostForm cannot be cancelled"
	client.Get("http://example.com")                         // want "Client\\)\\.Get cannot be cancelled"
	client.Head("http://example.com")                        // want "Client\\)\\.Head cannot be cancelled"
	client.Post("http://example.com", "text/plain", nil)     // want "Client\\)\\.Post cannot be cancelled"
	client.PostForm("http://example.com", nil)               // want "Client\\)\\.PostForm cannot be cancelled"
	_, _ = http.NewRequest("GET", "http://example.com", nil) // want "http\\.NewRequest cannot be cancelled"
	exec.Command("ls")                                       // want "exec\\.Command cannot be cancelled"
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
	_, _ = d.stmt.Query(1)
	d.sb.WriteString("x")
	fn := func() {}
	fn()
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://example.com", nil)
	_, _ = client.Do(req)
}
