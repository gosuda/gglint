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
	client.Get("http://example.com")                         // want "Client\\)\\.Get cannot be cancelled"
	_, _ = http.NewRequest("GET", "http://example.com", nil) // want "http\\.NewRequest cannot be cancelled"
	exec.Command("ls")                                       // want "exec\\.Command cannot be cancelled"
	db.Query("SELECT 1")                                     // want "DB\\)\\.Query cannot be cancelled"
	tx.Exec("SELECT 1")                                      // want "Tx\\)\\.Exec cannot be cancelled"
	db.Begin()                                               // want "DB\\)\\.Begin cannot be cancelled"
	db.Ping()                                                // want "DB\\)\\.Ping cannot be cancelled"
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
