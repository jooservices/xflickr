package roles

import (
	"encoding/base64"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	redisv9 "github.com/redis/go-redis/v9"

	"github.com/jooservices/xflickr/internal/platform/config"
	flickrclient "github.com/jooservices/xflickr/pkg/flickr"
)

// TestEveryRoleBuildsGovernorWithRebuilder fails when a production role still
// constructs a redis.Governor by struct literal instead of NewGovernor /
// newQuotaGovernor. That path is what left crawl without a rebuilder.
func TestEveryRoleBuildsGovernorWithRebuilder(t *testing.T) {
	dir := "."
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	var offenders []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		file, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			comp, ok := n.(*ast.CompositeLit)
			if !ok || len(comp.Elts) == 0 {
				return true
			}
			sel, ok := comp.Type.(*ast.SelectorExpr)
			if !ok || sel.Sel == nil {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || sel.Sel.Name != "Governor" {
				return true
			}
			if pkg.Name != "quotaRedis" && pkg.Name != "quotaredis" {
				return true
			}
			pos := fset.Position(comp.Pos())
			offenders = append(offenders, pos.Filename+":"+strconv.Itoa(pos.Line))
			return true
		})
	}
	if len(offenders) > 0 {
		t.Fatalf("roles must build governors via newQuotaGovernor/NewGovernor; found literals at %s", strings.Join(offenders, ", "))
	}

	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	cfg := config.Config{
		Crypto: config.CryptoConfig{MasterKeys: "test:" + key, ActiveMasterKeyID: "test"},
		Redis:  config.RedisConfig{Prefix: "guard:"},
	}
	client := redisv9.NewClient(&redisv9.Options{Addr: "127.0.0.1:1"})
	t.Cleanup(func() { _ = client.Close() })
	pool := &pgxpool.Pool{}
	engine, err := newCrawlEngine(pool, client, cfg, flickrclient.NewClient("http://127.0.0.1:1", nil))
	if err != nil || engine == nil || engine.Governor == nil {
		t.Fatalf("crawl engine = %+v err=%v", engine, err)
	}
	quota, err := newQuotaRuntime(pool, client, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if quota.Governor.Rebuilder == nil || quota.Rebuilder == nil {
		t.Fatal("scheduler quota runtime missing rebuilder")
	}
	if _, _, err := newAccountVerifier(pool, client, cfg); err != nil {
		t.Fatalf("account verifier: %v", err)
	}
}
