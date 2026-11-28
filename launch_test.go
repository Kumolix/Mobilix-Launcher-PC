package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"
)

// В launch.go словарь подстановок аргументов запуска должен содержать все нужные ключи
// (однажды комментарий в конце строки «съел» game_directory и assets_root).
func TestLaunchReplKeys(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "launch.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		cl, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		keys := map[string]bool{}
		for _, e := range cl.Elts {
			if kv, ok := e.(*ast.KeyValueExpr); ok {
				if bl, ok := kv.Key.(*ast.BasicLit); ok {
					s, _ := strconv.Unquote(bl.Value)
					keys[s] = true
				}
			}
		}
		if keys["version_name"] {
			found = true
			for _, k := range []string{"auth_player_name", "game_directory", "assets_root", "assets_index_name", "auth_uuid", "auth_access_token", "user_type", "version_type", "natives_directory", "classpath", "library_directory", "classpath_separator", "primary_jar"} {
				if !keys[k] {
					t.Errorf("в launch.go нет ключа подстановки %q", k)
				}
			}
		}
		return true
	})
	if !found {
		t.Fatal("словарь подстановок не найден")
	}
}
