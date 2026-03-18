package generator

import (
	"go/token"
	"path/filepath"
	"testing"

	"github.com/dave/dst"
	"github.com/phpboyscout/gtb/pkg/props"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
)

func TestExtractAliases(t *testing.T) {
	g := &Generator{}

	tests := []struct {
		name     string
		expr     dst.Expr
		expected []string
	}{
		{
			name: "CompositeLit with strings",
			expr: &dst.CompositeLit{
				Type: &dst.ArrayType{Elt: &dst.Ident{Name: "string"}},
				Elts: []dst.Expr{
					&dst.BasicLit{Kind: token.STRING, Value: "\"alias1\""},
					&dst.BasicLit{Kind: token.STRING, Value: "`alias2`"},
				},
			},
			expected: []string{"alias1", "alias2"},
		},
		{
			name:     "Not a CompositeLit",
			expr:     &dst.BasicLit{Kind: token.STRING, Value: "\"foo\""},
			expected: nil,
		},
		{
			name: "CompositeLit with mixed types (should ignore non-basic lit)",
			expr: &dst.CompositeLit{
				Elts: []dst.Expr{
					&dst.BasicLit{Kind: token.STRING, Value: "\"alias1\""},
					&dst.Ident{Name: "someVar"},
				},
			},
			expected: []string{"alias1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &ManifestCommand{}
			g.extractAliases(tt.expr, cmd)
			assert.Equal(t, tt.expected, cmd.Aliases)
		})
	}
}

func TestExtractArgs(t *testing.T) {
	g := &Generator{}

	tests := []struct {
		name     string
		expr     dst.Expr
		expected string
	}{
		{
			name: "SelectorExpr (e.g. cobra.NoArgs)",
			expr: &dst.SelectorExpr{
				X:   &dst.Ident{Name: "cobra"},
				Sel: &dst.Ident{Name: "NoArgs"},
			},
			expected: "NoArgs",
		},
		{
			name: "CallExpr (e.g. cobra.ExactArgs(1))",
			expr: &dst.CallExpr{
				Fun: &dst.SelectorExpr{
					X:   &dst.Ident{Name: "cobra"},
					Sel: &dst.Ident{Name: "ExactArgs"},
				},
				Args: []dst.Expr{
					&dst.BasicLit{Kind: token.INT, Value: "1"},
				},
			},
			expected: "ExactArgs(1)",
		},
		{
			name: "CallExpr with multiple args (e.g. cobra.MinimumNArgs(2))",
			expr: &dst.CallExpr{
				Fun: &dst.SelectorExpr{
					X:   &dst.Ident{Name: "cobra"},
					Sel: &dst.Ident{Name: "MinimumNArgs"},
				},
				Args: []dst.Expr{
					&dst.BasicLit{Kind: token.INT, Value: "2"},
				},
			},
			expected: "MinimumNArgs(2)",
		},
		{
			name: "CallExpr with non-basic lit arg (ignored)",
			expr: &dst.CallExpr{
				Fun: &dst.SelectorExpr{
					X:   &dst.Ident{Name: "cobra"},
					Sel: &dst.Ident{Name: "SomeFunc"},
				},
				Args: []dst.Expr{
					&dst.Ident{Name: "someVar"},
				},
			},
			expected: "SomeFunc()",
		},
		{
			name:     "Invalid expr type",
			expr:     &dst.BasicLit{Kind: token.STRING, Value: "\"foo\""},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &ManifestCommand{}
			g.extractArgs(tt.expr, cmd)
			assert.Equal(t, tt.expected, cmd.Args)
		})
	}
}

func TestIsAddCommandArg(t *testing.T) {
	g := &Generator{}

	// Create context
	ctx := &subcommandContext{
		pkgName:            "child",
		funcNameToBeCalled: "NewCmdChild",
		subCmdVar:          "childCmd",
	}

	tests := []struct {
		name     string
		arg      dst.Expr
		expected bool
	}{
		{
			name: "Inline call: child.NewCmdChild(props)",
			arg: &dst.CallExpr{
				Fun: &dst.SelectorExpr{
					X:   &dst.Ident{Name: "child"},
					Sel: &dst.Ident{Name: "NewCmdChild"},
				},
			},
			expected: true,
		},
		{
			name:     "Variable: childCmd",
			arg:      &dst.Ident{Name: "childCmd"},
			expected: true,
		},
		{
			name: "Incorrect package name in inline call",
			arg: &dst.CallExpr{
				Fun: &dst.SelectorExpr{
					X:   &dst.Ident{Name: "other"},
					Sel: &dst.Ident{Name: "NewCmdChild"},
				},
			},
			expected: false,
		},
		{
			name:     "Incorrect variable name",
			arg:      &dst.Ident{Name: "otherCmd"},
			expected: false,
		},
		{
			name:     "Invalid arg type",
			arg:      &dst.BasicLit{Kind: token.STRING, Value: `"foo"`},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, g.isAddCommandArg(tt.arg, ctx))
		})
	}
}

func TestHandleNewCmdRootInit(t *testing.T) {
	g := &Generator{}

	tests := []struct {
		name          string
		as            *dst.AssignStmt
		call          *dst.CallExpr
		expectedVar   string
		expectedReg   bool
		expectedIndex int
	}{
		{
			name: "Standard init: rootCmd := pkg.NewCmdRoot(props)",
			as: &dst.AssignStmt{
				Lhs: []dst.Expr{&dst.Ident{Name: "rootCmd"}},
			},
			call: &dst.CallExpr{
				Args: []dst.Expr{&dst.Ident{Name: "props"}},
			},
			expectedVar:   "rootCmd",
			expectedReg:   false,
			expectedIndex: 5, // arbitrary index
		},
		{
			name: "Init with existing subcommand: rootCmd := pkg.NewCmdRoot(props, child.NewCmdChild(p))",
			as: &dst.AssignStmt{
				Lhs: []dst.Expr{&dst.Ident{Name: "rootCmd"}},
			},
			call: &dst.CallExpr{
				Args: []dst.Expr{
					&dst.Ident{Name: "props"},
					&dst.CallExpr{
						Fun: &dst.SelectorExpr{
							X:   &dst.Ident{Name: "child"},
							Sel: &dst.Ident{Name: "NewCmdChild"},
						},
					},
				},
			},
			expectedVar:   "rootCmd",
			expectedReg:   true,
			expectedIndex: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &subcommandContext{
				pkgName:            "child",
				funcNameToBeCalled: "NewCmdChild",
			}
			// Reset for each test
			ctx.rootCmdInitIdx = -1
			ctx.cmdVarName = ""
			ctx.registered = false

			g.handleNewCmdRootInit(tt.as, tt.call, tt.expectedIndex, 0, ctx)

			assert.Equal(t, tt.expectedVar, ctx.cmdVarName)
			assert.Equal(t, tt.expectedReg, ctx.registered)
			assert.Equal(t, tt.expectedIndex, ctx.rootCmdInitIdx)
		})
	}
}

func TestHandleAllAssetsAssignment(t *testing.T) {
	g := &Generator{}

	t.Run("Initialize with make", func(t *testing.T) {
		ctx := &subcommandContext{capacity: 10}
		as := &dst.AssignStmt{}
		expr := &dst.CallExpr{
			Fun: &dst.Ident{Name: "make"},
			Args: []dst.Expr{
				&dst.Ident{Name: "Type"},
				&dst.BasicLit{Kind: token.INT, Value: "0"},
				&dst.BasicLit{Kind: token.INT, Value: "5"},
			},
		}

		g.handleAllAssetsAssignment(as, expr, 0, ctx)
		assert.True(t, ctx.allAssetsInitialized)
		// Check if capacity was updated in args[2]
		assert.Equal(t, "10", expr.Args[2].(*dst.BasicLit).Value)
	})

	t.Run("Variable declaration in root", func(t *testing.T) {
		ctx := &subcommandContext{
			isRoot:            true,
			firstAllAssetsIdx: -1,
		}
		as := &dst.AssignStmt{Tok: token.DEFINE}
		expr := &dst.Ident{Name: "something"}

		g.handleAllAssetsAssignment(as, expr, 5, ctx)

		assert.Contains(t, ctx.stmtIdxToRemove, 5)
		assert.Equal(t, 5, ctx.firstAllAssetsIdx)
	})
}

func TestCountCommandsWithAssets(t *testing.T) {
	tests := []struct {
		name     string
		commands []ManifestCommand
		expected int
	}{
		{
			name:     "No commands",
			commands: []ManifestCommand{},
			expected: 0,
		},
		{
			name: "Single command with assets",
			commands: []ManifestCommand{
				{Name: "cmd1", WithAssets: true},
			},
			expected: 1,
		},
		{
			name: "Single command without assets",
			commands: []ManifestCommand{
				{Name: "cmd1", WithAssets: false},
			},
			expected: 0,
		},
		{
			name: "Nested commands with mixed assets",
			commands: []ManifestCommand{
				{
					Name:       "parent",
					WithAssets: true,
					Commands: []ManifestCommand{
						{Name: "child1", WithAssets: false},
						{Name: "child2", WithAssets: true},
					},
				},
			},
			expected: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, countCommandsWithAssets(tt.commands))
		})
	}
}

func TestIsSubcommandAssetAppend(t *testing.T) {
	g := &Generator{}
	ctx := &subcommandContext{pkgName: "child"}

	tests := []struct {
		name     string
		stmt     dst.Stmt
		expected bool
	}{
		{
			name: "Correct append: allAssets = append(allAssets, childAssets...)",
			stmt: &dst.AssignStmt{
				Tok: token.ASSIGN,
				Lhs: []dst.Expr{&dst.Ident{Name: "allAssets"}},
				Rhs: []dst.Expr{
					&dst.CallExpr{
						Fun: &dst.Ident{Name: "append"},
						Args: []dst.Expr{
							&dst.Ident{Name: "allAssets"},
							&dst.Ident{Name: "childAssets"},
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "Incorrect LHS variable",
			stmt: &dst.AssignStmt{
				Tok: token.ASSIGN,
				Lhs: []dst.Expr{&dst.Ident{Name: "otherAssets"}},
				Rhs: []dst.Expr{
					&dst.CallExpr{
						Fun: &dst.Ident{Name: "append"},
					},
				},
			},
			expected: false,
		},
		{
			name: "Incorrect append argument (wrong package prefix)",
			stmt: &dst.AssignStmt{
				Tok: token.ASSIGN,
				Lhs: []dst.Expr{&dst.Ident{Name: "allAssets"}},
				Rhs: []dst.Expr{
					&dst.CallExpr{
						Fun: &dst.Ident{Name: "append"},
						Args: []dst.Expr{
							&dst.Ident{Name: "allAssets"},
							&dst.Ident{Name: "otherAssets"},
						},
					},
				},
			},
			expected: false,
		},
		{
			name:     "Not an assignment",
			stmt:     &dst.ExprStmt{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, g.isSubcommandAssetAppend(tt.stmt, ctx))
		})
	}
}

func TestDetectAssets(t *testing.T) {
	tests := []struct {
		name           string
		setupFS        func(fs afero.Fs, path string)
		fileContent    *dst.File // helper to construct minimal file with/without embed
		expectedAssets bool
	}{
		{
			name: "Assets directory exists",
			setupFS: func(fs afero.Fs, path string) {
				cmdDir := filepath.Dir(path)
				_ = fs.MkdirAll(filepath.Join(cmdDir, "assets"), 0755)
			},
			fileContent:    &dst.File{},
			expectedAssets: true,
		},
		{
			name:    "Embed comment present",
			setupFS: func(fs afero.Fs, path string) {},
			fileContent: &dst.File{
				Decls: []dst.Decl{
					&dst.GenDecl{
						Tok: token.VAR,
						Specs: []dst.Spec{
							&dst.ValueSpec{
								Decs: dst.ValueSpecDecorations{
									NodeDecs: dst.NodeDecs{
										Start: dst.Decorations{"//go:embed assets/*"},
									},
								},
							},
						},
					},
				},
			},
			expectedAssets: true,
		},
		{
			name:           "No assets",
			setupFS:        func(fs afero.Fs, path string) {},
			fileContent:    &dst.File{},
			expectedAssets: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			path := "/work/pkg/cmd/test/cmd.go"
			_ = fs.MkdirAll(filepath.Dir(path), 0755)

			if tt.setupFS != nil {
				tt.setupFS(fs, path)
			}

			g := &Generator{
				props: &props.Props{FS: fs},
			}

			cmd := &ManifestCommand{}
			g.detectAssets(path, tt.fileContent, cmd)

			assert.Equal(t, tt.expectedAssets, cmd.WithAssets)
		})
	}
}

func TestCheckPropsAssignment(t *testing.T) {
	g := &Generator{}

	tests := []struct {
		name        string
		stmt        *dst.AssignStmt
		expr        dst.Expr
		idx         int
		expectedVar string
	}{
		{
			name: "Valid Props Assignment",
			stmt: &dst.AssignStmt{
				Lhs: []dst.Expr{&dst.Ident{Name: "myProps"}},
				Rhs: []dst.Expr{},
			},
			expr: &dst.UnaryExpr{
				Op: token.AND,
				X: &dst.CompositeLit{
					Type: &dst.SelectorExpr{
						X:   &dst.Ident{Name: "pkg"},
						Sel: &dst.Ident{Name: "Props"},
					},
				},
			},
			idx:         0,
			expectedVar: "myProps",
		},
		{
			name: "Not Props (Wrong Name)",
			stmt: &dst.AssignStmt{
				Lhs: []dst.Expr{&dst.Ident{Name: "other"}},
			},
			expr: &dst.UnaryExpr{
				Op: token.AND,
				X: &dst.CompositeLit{
					Type: &dst.SelectorExpr{
						X:   &dst.Ident{Name: "pkg"},
						Sel: &dst.Ident{Name: "NotProps"},
					},
				},
			},
			idx:         0,
			expectedVar: "",
		},
		{
			name: "Not Selector (Local Props)",
			stmt: &dst.AssignStmt{
				Lhs: []dst.Expr{&dst.Ident{Name: "local"}},
			},
			expr: &dst.UnaryExpr{
				Op: token.AND,
				X: &dst.CompositeLit{
					Type: &dst.Ident{Name: "Props"},
				},
			},
			idx:         0,
			expectedVar: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &subcommandContext{}
			g.checkPropsAssignment(tt.stmt, tt.expr, tt.idx, ctx)
			assert.Equal(t, tt.expectedVar, ctx.propsVarName)
		})
	}
}

func TestCheckOptsAssignment(t *testing.T) {
	g := &Generator{}

	tests := []struct {
		name        string
		stmt        *dst.AssignStmt
		expr        dst.Expr
		idx         int
		expectedVar string
	}{
		{
			name: "Valid Options Assignment (Suffix)",
			stmt: &dst.AssignStmt{
				Lhs: []dst.Expr{&dst.Ident{Name: "myOpts"}},
			},
			expr: &dst.UnaryExpr{
				Op: token.AND,
				X: &dst.CompositeLit{
					Type: &dst.SelectorExpr{
						X:   &dst.Ident{Name: "pkg"},
						Sel: &dst.Ident{Name: "ServerOptions"},
					},
				},
			},
			idx:         0,
			expectedVar: "myOpts",
		},
		{
			name: "Not Options (Wrong Suffix)",
			stmt: &dst.AssignStmt{
				Lhs: []dst.Expr{&dst.Ident{Name: "other"}},
			},
			expr: &dst.UnaryExpr{
				Op: token.AND,
				X: &dst.CompositeLit{
					Type: &dst.SelectorExpr{
						X:   &dst.Ident{Name: "pkg"},
						Sel: &dst.Ident{Name: "ServerConfig"},
					},
				},
			},
			idx:         0,
			expectedVar: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &subcommandContext{}
			g.checkOptsAssignment(tt.stmt, tt.expr, tt.idx, ctx)
			assert.Equal(t, tt.expectedVar, ctx.optsVarName)
		})
	}
}

func TestCheckAllAssetsInitialized(t *testing.T) {
	g := &Generator{}

	t.Run("Variable 'allAssets' declared", func(t *testing.T) {
		ctx := &subcommandContext{}
		stmt := &dst.DeclStmt{
			Decl: &dst.GenDecl{
				Tok: token.VAR,
				Specs: []dst.Spec{
					&dst.ValueSpec{
						Names: []*dst.Ident{
							{Name: "allAssets"},
						},
						Type: &dst.Ident{Name: "[]fs.FS"},
					},
				},
			},
		}

		g.checkAllAssetsInitialized(stmt, ctx)
		assert.True(t, ctx.allAssetsInitialized)
	})

	t.Run("Other variable declared", func(t *testing.T) {
		ctx := &subcommandContext{}
		stmt := &dst.DeclStmt{
			Decl: &dst.GenDecl{
				Tok: token.VAR,
				Specs: []dst.Spec{
					&dst.ValueSpec{
						Names: []*dst.Ident{
							{Name: "otherVar"},
						},
						Type: &dst.Ident{Name: "int"},
					},
				},
			},
		}

		g.checkAllAssetsInitialized(stmt, ctx)
		assert.False(t, ctx.allAssetsInitialized)
	})

	t.Run("Not a GenDecl", func(t *testing.T) {
		ctx := &subcommandContext{}
		stmt := &dst.DeclStmt{
			Decl: &dst.BadDecl{},
		}

		g.checkAllAssetsInitialized(stmt, ctx)
		assert.False(t, ctx.allAssetsInitialized)
	})
}

func TestProcessAssetsVarDecl(t *testing.T) {
	g := &Generator{}

	t.Run("Variable 'assets' declared", func(t *testing.T) {
		ctx := &subcommandContext{}
		decl := &dst.GenDecl{
			Tok: token.VAR,
			Specs: []dst.Spec{
				&dst.ValueSpec{
					Names: []*dst.Ident{
						{Name: "assets"},
					},
					Type: &dst.Ident{Name: "embed.FS"},
				},
			},
		}

		g.processAssetsVarDecl(decl, ctx)
		assert.Equal(t, "assets", ctx.assetsVarName)
	})

	t.Run("Other variable declared", func(t *testing.T) {
		ctx := &subcommandContext{}
		decl := &dst.GenDecl{
			Tok: token.VAR,
			Specs: []dst.Spec{
				&dst.ValueSpec{
					Names: []*dst.Ident{
						{Name: "other"},
					},
				},
			},
		}

		g.processAssetsVarDecl(decl, ctx)
		assert.Empty(t, ctx.assetsVarName)
	})
}

func TestRemoveMarkedStatements(t *testing.T) {
	g := &Generator{}

	t.Run("Remove statements and update index", func(t *testing.T) {
		ctx := &subcommandContext{
			stmtIdxToRemove: []int{1, 3}, // Remove index 1 and 3
			rootCmdInitIdx:  5,
		}

		fn := &dst.FuncDecl{
			Body: &dst.BlockStmt{
				List: []dst.Stmt{
					&dst.ExprStmt{}, // 0
					&dst.ExprStmt{}, // 1 - to remove
					&dst.ExprStmt{}, // 2
					&dst.ExprStmt{}, // 3 - to remove
					&dst.ExprStmt{}, // 4
					&dst.ExprStmt{}, // 5 - init
					&dst.ExprStmt{}, // 6
				},
			},
		}

		g.removeMarkedStatements(fn, ctx)

		assert.Len(t, fn.Body.List, 5)
		// expected index: started at 5. Removed 3 (below 5) -> 4. Removed 1 (below 4) -> 3.
		assert.Equal(t, 3, ctx.rootCmdInitIdx)
	})
}

func TestAnalyzeExprStmt(t *testing.T) {
	g := &Generator{}

	t.Run("AddCommand call", func(t *testing.T) {
		ctx := &subcommandContext{
			pkgName:            "child",
			funcNameToBeCalled: "NewCmdChild",
		}
		stmt := &dst.ExprStmt{
			X: &dst.CallExpr{
				Fun: &dst.SelectorExpr{
					X:   &dst.Ident{Name: "rootCmd"},
					Sel: &dst.Ident{Name: "AddCommand"},
				},
				Args: []dst.Expr{
					&dst.CallExpr{
						Fun: &dst.SelectorExpr{
							X:   &dst.Ident{Name: "child"},
							Sel: &dst.Ident{Name: "NewCmdChild"},
						},
						Args: []dst.Expr{&dst.Ident{Name: "p"}},
					},
				},
			},
		}

		g.analyzeExprStmt(stmt, ctx)
		assert.True(t, ctx.registered)
	})

	t.Run("Other call", func(t *testing.T) {
		ctx := &subcommandContext{}
		stmt := &dst.ExprStmt{
			X: &dst.CallExpr{
				Fun: &dst.Ident{Name: "SomeFunction"},
			},
		}

		g.analyzeExprStmt(stmt, ctx)
		assert.False(t, ctx.registered)
	})
}

func TestAppendSubcommandCallToRootInit(t *testing.T) {
	g := &Generator{}
	ctx := &subcommandContext{
		pkgName:            "child",
		funcNameToBeCalled: "NewCmdChild",
	}

	// Create AST for root function: func NewCmdRoot(props *props.Props) *cobra.Command
	fn := &dst.FuncDecl{
		Name: dst.NewIdent("NewCmdRoot"),
		Body: &dst.BlockStmt{
			List: []dst.Stmt{
				&dst.AssignStmt{
					Lhs: []dst.Expr{dst.NewIdent("cmd")},
					Rhs: []dst.Expr{
						&dst.CallExpr{
							Fun: &dst.SelectorExpr{
								X:   dst.NewIdent("pkg"),
								Sel: dst.NewIdent("NewCmdRoot"),
							},
							Args: []dst.Expr{dst.NewIdent("props")},
						},
					},
				},
			},
		},
	}

	g.appendSubcommandCallToRootInit(fn, ctx)

	// Verify that child.NewCmdChild(p) is appended to args
	as := fn.Body.List[0].(*dst.AssignStmt)
	call := as.Rhs[0].(*dst.CallExpr)
	assert.Len(t, call.Args, 2)
	newArg := call.Args[1].(*dst.CallExpr)
	assert.Equal(t, "child", newArg.Fun.(*dst.SelectorExpr).X.(*dst.Ident).Name)
}

func TestInsertIntoRoot(t *testing.T) {
	g := &Generator{}
	ctx := &subcommandContext{
		pkgName:            "child",
		funcNameToBeCalled: "NewCmdChild",
	}

	// Create AST for root function
	fn := &dst.FuncDecl{
		Name: dst.NewIdent("NewCmdRoot"),
		Body: &dst.BlockStmt{
			List: []dst.Stmt{
				&dst.AssignStmt{
					Lhs: []dst.Expr{dst.NewIdent("cmd")},
					Rhs: []dst.Expr{
						&dst.CallExpr{
							Fun: &dst.SelectorExpr{
								X:   dst.NewIdent("pkg"),
								Sel: dst.NewIdent("NewCmdRoot"),
							},
							Args: []dst.Expr{dst.NewIdent("props")},
						},
					},
				},
			},
		},
	}

	g.insertIntoRoot(fn, ctx)

	// insertIntoRoot calls appendSubcommandCallToRootInit
	as := fn.Body.List[0].(*dst.AssignStmt)
	call := as.Rhs[0].(*dst.CallExpr)
	assert.Len(t, call.Args, 2)
}
