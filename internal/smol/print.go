package smol

import (
	"go/ast"
	"go/format"
	"go/token"
	"io"
)

func printerNode(w io.Writer, n ast.Node) error { return format.Node(w, token.NewFileSet(), n) }
