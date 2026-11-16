# Smol Language Specification

Version 0.2 (draft)

Smol is a compact surface syntax for Go. A Go package converts to Smol (**lifting**) and back (**lowering**) with no loss. Smol exists so that language models can read and write Go programs in about half the tokens; people and the Go toolchain keep working with the Go form.

Smol has no semantics of its own. A Smol program means exactly what its lowered Go means.

Version 0.1 kept everything in one file and cut a real package by 12%. Version 0.2 changes three things to reach 50% (§2):

1. A Smol file is split into two **channels**. The code channel holds the program. The notes channel holds comments and blank lines. Both are needed to reproduce the Go byte for byte; only the code channel is needed to read, change or run the program.
2. The code channel is **dense**: no optional whitespace.
3. Names that the surrounding scope already determines are left out (§6.10–§6.13).

## 1. Goals and non-goals

Goals:

1. **Token efficiency.** Minimise the LLM tokens needed to load a complete program.
2. **Lossless round trip.** Go → Smol → Go reproduces the normalised Go source byte for byte from the two channels together (§3).
3. **A complete program in the code channel.** Lowering the code channel alone gives the same Go program without its comments and blank lines. Compiler directives stay in the code channel.
4. **Idiomatic output.** Lowered Go is what a careful Go programmer would write.
5. **One canonical form.** Every Go program has exactly one canonical Smol spelling.
6. **Small delta from Go.** Anything not listed in §5 and §6 is written exactly as in Go.

Non-goals:

- New semantics, a type system, a runtime, or a standard library.
- Human readability of the code channel. Humans review the Go.
- Renaming identifiers or rewriting literals. They are preserved verbatim. A measured alias table for long identifiers was rejected (§2.3).

## 2. Design basis

All counts are `o200k_base` tokens. That tokenizer reproduces the input usage that Codex reports for `gpt-6.1-sol` to within one token on every bundle measured, so it is used for local iteration. The figures marked *measured* are real request usage (§10).

### 2.1 Why version 0.1 fell short

The benchmark is the complete `encoding/json` package of Go 1.26.3: 8 files, 4,264 lines, 32,909 tokens.

| Kind of Go token | Share |
|---|---|
| Comments | 30.5% |
| Punctuation and operators | 23.9% |
| Identifiers | 21.8% |
| Newlines and indentation | 12.0% |
| Literals | 7.7% |
| Keywords | 4.1% |

Version 0.1 removed indentation and folded error handling, which `encoding/json` uses little. Comments are prose and do not compress, so a format that keeps them in view cannot remove half of this package: the code would have to shrink by 72%.

| Stage | Tokens | Reduction |
|---|---|---|
| Go | 32,909 | |
| Smol 0.1, one channel | 28,898 | 12.2% *measured* |
| Comments and blank lines moved to the notes channel | 18,708 | 43.2% |
| Dense layout and implied line breaks (§5) | 17,516 | 46.8% |
| Scope elision, implied commas, partial import blocks (§6.1, §6.10–§6.13), directives kept in code | 16,377 | 50.2% *measured* |

### 2.2 What costs tokens

| Finding | Consequence |
|---|---|
| `func`, `return`, `string`, `:=`, `range` are already one token each. | Smol does not rename keywords or types. |
| A newline and the indentation after it are separate tokens. | No indentation. |
| ` {\n` and `}\n` are one token each; indentation-delimited blocks measured within 2% of braces. | Smol keeps braces. |
| Removing every optional space saves 6% of code tokens. A tokenizer-driven choice of spaces saves 0.3% more. | A fixed rule: no optional spaces (§5). |
| After that, code costs about one token per Go lexeme (17.8k lexemes, 17.5k tokens). | Further savings must remove lexemes, not re-spell them. |
| Qualifiers repeat: `reflect.` 268 times, a receiver or state parameter before most field uses, the same parameter types in every signature. | Scope elision (§6.10–§6.12). |

### 2.3 Measured and rejected

| Idea | Effect on the benchmark | Why not |
|---|---|---|
| Alias table for long identifiers (`ute=UnmarshalTypeError`) | −783 tokens (2.4%) | The code becomes opaque; names are the main thing a reader has. |
| Implied line break before any identifier-led statement | −41 | Needs a statement parser; most lines already end in a merged `)\n` or `}\n`. |
| Merging consecutive closing braces | −53 | Loses the layout of composite literals. |
| Bare promoted members (`e.WriteString` through an embedded type) | −64 | Needs full type information on both sides. |
| Dropping the spaces around implied commas | −18 | Kept for legibility. |

## 3. Round-trip contract

Let `N(g)` be Go source `g` after normalisation:

1. `gofmt` (without `-s`).
2. Imports merged into one declaration, sorted, in two groups separated by a blank line: standard library, then everything else. Imports with comments, and `import "C"` with its preamble, keep their own declarations.

`lift(g)` returns a pair `(code, notes)`. Conforming tools satisfy:

- **L1.** `lower(code, notes) == N(g)` for every Go package `g` that parses.
- **L2.** `lift(lower(c, n)) == fmt(c, n)` for every valid Smol file.
- **L3.** `fmt(fmt(c, n)) == fmt(c, n)`, and `lift` always produces canonical Smol.
- **L4.** `lower(code, "")` has the same Go tokens and compiler directives as `N(g)`. Only comments, blank lines and the alignment that depends on them differ.

Every form in §6 is an exact syntactic template. Lifting applies a form if and only if the Go matches the template; anything else stays in Go spelling. A comment or blank line inside a template's span prevents the fold.

Two forms need declaration-level information: zero values (§7) need the underlying kind of named result types, and scope elision (§6.10, §6.11) needs the exported names of opened packages and the members of package types. Neither needs expression types.

Lifting must verify scope elision: if a file lifted with §6.10–§6.13 does not lower back to `N(g)`, the file is lifted without those four forms.

## 4. Files, channels and tools

- `name.smol` is the code channel for `name.go`, including `_test` and build-constraint suffixes.
- `name.smol.notes` is the notes channel. It is absent when there is nothing to record.
- The unit of translation is the package, because import inference and scope elision need the package scope.
- `smol.mod` sits beside `go.mod` and holds the module's import table (§6.1).
- Smol tracks the Go version declared in `go.mod`. This version requires Go 1.22 or later.

| Command | Effect |
|---|---|
| `smol lift [packages]` | Go → code and notes; adds missing entries to `smol.mod` |
| `smol lower [packages]` | code and notes → Go; notes are optional |
| `smol fmt [files]` | Rewrite both channels in canonical form |
| `smol check [packages]` | Verify L1–L3 for the given packages |
| `smol outline [-docs] [packages]` | Print the outline view (§9) |

`smol lower -lines` adds `//line` directives that refer to the code channel. Output produced with `-lines` is exempt from L1.

### 4.1 The notes channel

The notes channel records every comment and blank line by position, so that the code channel carries none.

The code channel is divided into **segments**. A segment starts at each line that begins outside all brackets, so each top-level declaration is a segment. A segment is identified by its **head**: its first line up to the first `{`, at most ten lexemes, in dense spelling. A repeated head gets the suffix ` #2`, ` #3`, and so on.

```
smol-notes 1
@38 (s*Server)Get(ctx,id int64)
0 c20 "// Get returns the user with the given id."
0 e1
@50 .Emails(ctx)[]string!
0 b
```

`@count head` opens a segment and gives its number of lexemes. Each following line is `k entry`, where `k` is the index of a lexeme in the segment and the entry describes the gap before that lexeme:

| Entry | Meaning |
|---|---|
| `b` | a blank line |
| `c<n><t> "text"` | a comment after `n` line breaks (0–2), tab-indented if `t` is 1; the text is a Go string literal |
| `d<n><t>` | the next comment that is written in the code channel (a directive) goes here |
| `e<n>` | `n` line breaks between the last comment and the lexeme |

Index `count` addresses the end of the last segment, for comments at the end of the file. A doc comment is at `k = 0` of the declaration it documents.

**Directives.** Comments that change what the toolchain builds stay in the code channel on their own lines: `//go:` directives, `//line`, `//export`, `//extern`, `// +build`, and the cgo preamble before `import "C"`.

**Edited code.** Notes are exact for a segment whose head and lexeme count are unchanged. When a segment's lexeme count differs from the notes, its `k = 0` entries stay in place and its other comments are placed on their own lines above the declaration; `lower` reports this. Notes whose head matches no segment are reported and dropped. A comment written directly in the code channel is kept where it is and moves to the notes on the next `fmt`.

## 5. Lexical structure and layout

Smol uses Go's lexical grammar: identifiers, keywords, literals, operators, comments and the semicolon-insertion rule are unchanged. Smol adds **no reserved words**, so every Go identifier is a valid Smol identifier.

New tokens: `?`, `=>`, `@`, `\`. The word `in` is a keyword only inside a `for` header (§6.7). The words `use` and `let` are keywords only at the start of a line directly after the package clause (§6.10, §6.12).

Canonical layout of the code channel (`smol fmt`):

1. No comments except directives (§4.1), and no blank lines.
2. No indentation.
3. **Dense spacing.** Two lexemes on a line are separated by one space only if they would otherwise scan as different lexemes (`a b`, `! =`), or if one is a quoted literal and the other side is a word, number or quoted literal with nothing between them (`f(w "bad id" code)`).
4. Line breaks are where the corresponding Go has them, except as rules 5 and 6 and §6 state.
5. **Implied line breaks.** Inside any bracket, a line break before `if`, `for`, `switch`, `select`, `return`, `var`, `go`, `defer`, `break`, `continue`, `goto` or `fallthrough` is written as a space when the preceding lexeme ends in a letter, digit, underscore or quote and is not `else`, `case` or one of those keywords. Reading reverses this: such a keyword after such a lexeme on the same line starts a new line. So `y:=x if y>0{` is two statements.
6. **Inline blocks.** A block that belongs to `if`, `else`, `for` or a `?` handler, and that holds exactly one statement containing no block and no comment, is written on the header line: `if id<=0{!ErrNotFound}`. gofmt never emits this layout for these blocks, so lowering always expands it. Function bodies keep their one-line or multi-line layout as written.

Parsers must accept any indentation and spacing, and comments anywhere. Only the printer is strict. The examples in §6 are spaced for legibility.

## 6. Differences from Go

Everything not listed here is written exactly as in Go. Each subsection gives the Smol form, its Go expansion, and the condition under which lifting uses it. Lifting must use a form whenever its condition holds.

### 6.1 Implicit imports

A Smol file lists only the imports that cannot be inferred.

An identifier `q` in a selector `q.X` that is not declared in any enclosing scope, the package scope or the universe scope is a package reference. It resolves in this order:

1. An explicit `import` declaration in the file (Go syntax).
2. The `smol.mod` table.
3. The standard library table: every standard package whose final path element is unique within the standard library of the module's Go version. `rand`, `template`, `scanner` and `pprof` are examples of names that are not unique and so are never in the table.

Lowering emits one import per resolved qualifier, laid out as in §3. Qualifiers restored by §6.10 count.

A parenthesised import block that has no blank lines, and whose only comments are the doc or line comments of its own specs, keeps the specs that cannot be inferred after a `...` line that stands for the inferred ones:

```
import (
...
_ "unsafe" // for linkname
)
```

`smol.mod` format:

```
use yaml gopkg.in/yaml.v3
use chi github.com/go-chi/chi/v5
use pb = github.com/acme/api/gen/proto
```

`use name path` records that `path` declares package `name`. `use name = path` records an alias and lowers to `import name "path"`.

Lifting omits an import if it is not blank, not dot, has no comment, and resolves to the same path through step 2 or 3. `smol lift` adds a `use` line for each non-standard import whose qualifier is not yet in the table. A qualifier that is already bound to a different path stays explicit in the file.

### 6.2 Function declarations

Top-level function and method declarations omit `func`. Function literals and function types keep it.

```
Sum(xs []int) int {            // func Sum(xs []int) int {
(s *Server) Close()! {         // func (s *Server) Close() error {
Map[T, U any](xs []T, f func(T) U) []U {
```

A top-level declaration that starts with an identifier, `(` or `.` is a function declaration.

**Receiver inheritance.** A method declaration that starts with `.` has the same receiver as the nearest preceding method declaration in the file.

```
(s *Server) Get(ctx, id int64) *User! {
…
}

.Emails(ctx) []string! {       // func (s *Server) Emails(…
```

Lifting uses `.` when the receiver is token-for-token identical to that of the preceding method declaration.

### 6.3 Error results: `!`

A `!` after the result position stands for a final, unnamed `error` result.

| Smol | Go |
|---|---|
| `f()!` | `func f() error` |
| `f() *User!` | `func f() (*User, error)` |
| `f() (int, string)!` | `func f() (int, string, error)` |

It applies to function declarations, interface methods, function types and function literals. Named results use Go syntax. When `=` follows the marker it must be separated by a space (`var f func()! = g`), because `!=` lexes as one token.

A function whose last result is `error` is called **fallible** below.

### 6.4 Conventional parameters

In any parameter list, and as a receiver, these bare names carry a type. A file can add to the table (§6.12).

| Name | Type |
|---|---|
| `ctx` | `context.Context` |
| `w` | `http.ResponseWriter` |
| `r` | `*http.Request` |
| `t` | `*testing.T` |

A conventional parameter never shares the type of a following parameter. `(ctx, id int64)` lowers to `(ctx context.Context, id int64)`, and `(w, r)` lowers to `(w http.ResponseWriter, r *http.Request)`.

A list consisting only of bare identifiers is a list of conventional parameters if every identifier is in the table; otherwise it is a list of unnamed types, as in Go.

`\name` suppresses the convention and gives the name its Go meaning. `(\ctx, id string)` is Go's `(ctx, id string)`, and `func(\t)` is a function taking one value of a type named `t`.

Lifting writes a parameter bare when it is alone in its group, its name is in the table, and its type is spelled exactly as in the table. It writes `\` on any other table name that is bare in the Go source.

### 6.5 Error handling: `?`

A `?` clause attaches an error handler to a statement. The error variable is always named `err`.

Statement forms, where `E` is an expression and `B` is the handler body:

| Smol | Go |
|---|---|
| `a, b := E` + handler | `a, b, err := E` ⏎ `if err != nil { B }` |
| `a, b = E` + handler | `a, b, err = E` ⏎ `if err != nil { B }` |
| `err := E` + handler, `err = E` + handler | the same, with nothing appended to the left side |
| `E` + handler | `if err := E; err != nil { B }` |

Handlers:

| Smol | `B` |
|---|---|
| `?` | `return Z, err` |
| `? "format", args…` | `return Z, fmt.Errorf("format: %w", args…, err)` |
| `? { statements }` | `statements` |

`Z` is the zero value of each result before the last (§7). The first two handlers are valid only in a fallible function. The format must be an interpreted string literal; lowering inserts `: %w` before its closing quote. In the block handler, `err` is in scope.

```
u := s.store.Find(ctx, id)?
u := s.store.Find(ctx, id) ? "find user %d", id
tx.Commit()?
id := strconv.Atoi(s) ? {
http.Error(w, "bad id", http.StatusBadRequest)
return
}
```

`?` is a statement clause, not an operator: it cannot appear inside an expression.

Lifting folds two adjacent statements when the second is `if err != nil` with no `else`, and prefers the handlers in the order listed.

### 6.6 Returns in fallible functions: `^` and `!`

| Smol | Go | Condition |
|---|---|---|
| `^a, b` | `return a, b, nil` | fallible function |
| `^` | `return nil` | fallible function with one result |
| `!e` | `return Z, e` | fallible function with two or more results |

Both are statements and appear only at the start of a statement, where Go allows neither a unary `^` nor a unary `!`. Inside a function literal they refer to the literal.

Lifting, for a `return` in a fallible function whose value count matches the result count: if the last value is the identifier `nil`, use `^`; otherwise, if there are two or more results and every earlier value is the canonical zero value, use `!`; otherwise keep `return`.

### 6.7 Range loops: `in`

| Smol | Go |
|---|---|
| `for k, v in xs {` | `for k, v := range xs {` |
| `for v in xs {` | `for _, v := range xs {` |

All other loops use Go syntax, including `for i := range xs`, `for range xs` and range with `=`.

### 6.8 Expression bodies: `=>`

`=> expr` replaces a body that consists of exactly one `return` statement with one or more values and no comments.

| Context | `=>` stands for |
|---|---|
| Function declaration | the multi-line body `{ ⏎ return expr ⏎ }` |
| Function literal | the one-line body `{ return expr }` |

```
(u *User) FullName() string => u.FirstName + " " + u.LastName
sort.Slice(xs, func(i, j int) bool => xs[i].Age < xs[j].Age)
```

The other layout of each (a one-line declaration, a multi-line literal) is written with braces as in Go.

Braces are also required when removing them would lose the boundary between a literal's return expressions and its surrounding expression. In particular, keep braces on a literal immediately followed by a call, index, selector, type assertion or binary operator. Keep them on a tuple-returning literal followed by an outer comma when its return statement has fewer expressions than its result count (for example, `return pair()`). Parentheses around the whole literal delimit it and allow an arrow inside them.

Thus `func() any { return g }()` keeps its braces, while `func() any { return g() }` becomes `func()any=>g()`. These must remain distinct: the former invokes the literal and the latter denotes a function value. All unambiguous bodies satisfying the rules above use `=>`.

### 6.9 Struct tag directives: `@key`

One or more `@key` directives between `struct` and `{` give every exported, single-name field the tag `key:"snake_name"`, with multiple keys joined by one space in directive order.

```
type User struct @json @db {
ID int64                              // `json:"id" db:"id"`
FirstName string                      // `json:"first_name" db:"first_name"`
Email string `json:"email,omitempty"` // explicit tag replaces the implied one
Cache map[string]int ~                // no tag
secret string                         // unexported: no tag
}
```

`snake_name` is computed by splitting the field name before each upper-case letter that follows a lower-case letter or digit, and before the last upper-case letter of a run that is followed by a lower-case letter, then lower-casing and joining with `_`. So `ID` → `id`, `UserID` → `user_id`, `HTTPServer` → `http_server`.

An explicit tag replaces the implied tag entirely. `~` in the tag position means the field has no tag.

Lifting takes the key sequence from the first exported single-name field whose whole tag has the implied form, and uses the directive if at least two fields have exactly the implied tag for that sequence.

### 6.10 Opened packages: `use`

A `use` line directly after the package clause opens packages. Their exported names can be written bare.

```
package json
use reflect strconv

if rv.Kind() != Pointer { return Quote(name) }   // reflect.Pointer, strconv.Quote
```

Each name on the line is a package qualifier, resolved as in §6.1.

A bare identifier is **free** where it stands if it refers to no declaration in scope in the file, no package-level declaration of the package, no predeclared identifier, and is not the name of a package in the import table or the file's imports. Lowering rewrites a free identifier `X` as follows:

1. Inside a body with a subject that has the member `X`, it is `subject.X` (§6.11).
2. Otherwise, if exactly one opened package exports `X`, it is `pkg.X`. If more than one does, the file is invalid.
3. Otherwise it is left alone.

Lifting considers every selector `pkg.X` whose `pkg` is an imported package and whose `X` would be free in that position. It opens each package that has at least two such selectors, in name order, and writes `X` bare where exactly one opened package exports it. A package with no resulting bare name is not listed.

A selector is never written bare when it is the key of a composite literal element, when `X` is `in`, `use`, `let` or `_`, or in a file that has a dot import. Rule 1 takes precedence, so inside a body whose subject has a member `X`, `pkg.X` stays qualified.

### 6.11 Subject members

A function body has a **subject** when the function is a method with a named receiver, or when its first parameter is named and has a type declared in the package, or a pointer to one. The subject is that receiver or parameter.

Inside the body, including nested function literals, a member of the subject can be written bare. The members are the fields declared directly in the subject's struct type, including embedded field names, and the methods declared on the type in the package. Promoted members are not included.

```
(d *decodeState) init(data []byte) *decodeState {
d.data = data          // data is a parameter here, so the member stays qualified
off = 0                // d.off = 0
savedError = nil       // d.savedError = nil
return d
}

stateBeginString(s *scanner, c byte) int {
if c == '"' {
step = stateInString   // s.step = stateInString
return scanBeginLiteral
}
return s.error(c "looking for beginning of object key string")
}
```

A member is written bare only where its name is free (§6.10). In the first example `data` is a parameter, so `d.data` stays qualified. In the second, `error` is predeclared, so `s.error` stays qualified.

Lifting writes `subject.X` as `X` when the selector's operand is the subject itself and not a variable that shadows it, `X` is a member, `X` is free in that position, and the selector is not the key of a composite literal element.

### 6.12 File parameter conventions: `let`

A `let` line after the package clause and any `use` line adds entries to the table of §6.4 for the file. An entry for a name that is already in the table replaces it.

```
let c byte, opts encOpts, s *scanner, v reflect.Value

stateBeginValue(s, c) int {              // func stateBeginValue(s *scanner, c byte) int {
(s) pushParseState(c, newParseState int, successState int) int {
```

The rules of §6.4 apply unchanged, including `\name`. A receiver is a parameter list of one, so `(s)` is the receiver `(s *scanner)`.

Lifting counts, for each name, the types it has where it is the only name of a parameter or of a written receiver; a receiver that §6.2 writes as `.` is not counted. A name gets an entry if its most frequent type occurs at least three times (ties go to the type that sorts first) and that type is not already its convention. Entries are sorted by name. Types on the `let` line are written in full: §6.10 does not apply to them.

### 6.13 Implied commas

In the argument list of a call, and in the expression list of a `case`, the comma between two elements on the same line is omitted when the first ends in, and the second begins with, an identifier or a basic literal.

```
append(dst b)                    // append(dst, b)
pushParseState(c parseObjectKey scanBeginObject)
strconv.ParseInt(f(s), 10 64)    // the comma after ")" stays
case 0 1:
max(n, -1)                       // the second element begins with an operator
```

Lowering inserts a comma between two adjacent lexemes of those kinds directly inside a call's parentheses or a `case` list. No Go expression or type places two such lexemes side by side there: in `make([]int n)` the pair `] int` is not affected and `int n` gets its comma. Parameter lists, result lists, composite literals, and assignment and `return` lists keep their commas.

### 6.14 Grammar summary

Changes to the Go grammar, in the Go specification's notation:

```
SourceFile    = PackageClause [ UseLine ] [ LetLine ] { ImportDecl } { TopLevelDecl } .
UseLine       = "use" identifier { identifier } .
LetLine       = "let" identifier Type { "," identifier Type } .
ImportDecl    = "import" ( ImportSpec | "(" [ "..." ";" ] { ImportSpec ";" } ")" ) .
TopLevelDecl  = Declaration | FuncDecl .
FuncDecl      = [ Receiver | "." ] identifier [ TypeParameters ] Signature [ FuncBody ] .
FuncBody      = Block | "=>" Expression .
FunctionLit   = "func" Signature FuncBody .
Signature     = Parameters [ Result ] [ "!" ] .
ParameterDecl = [ "\" ] identifier | [ IdentifierList ] [ "..." ] Type .
StructType    = "struct" { "@" identifier } "{" { FieldDecl ";" } "}" .
FieldDecl     = ( IdentifierList Type | EmbeddedField ) [ Tag | "~" ] .

Statement     = … | TryStmt | OkStmt | FailStmt .
TryStmt       = ( Expression | ExpressionList ( ":=" | "=" ) Expression ) Handler .
Handler       = "?" [ string_lit { "," Expression } | Block ] .
OkStmt        = "^" [ ExpressionList ] .
FailStmt      = "!" Expression .
RangeClause   = … | IdentifierList "in" Expression .
Arguments     = "(" [ Expression { [ "," ] Expression } [ "..." ] [ "," ] ] ")" .
ExprCaseClause = "case" Expression { [ "," ] Expression } ":" StatementList .
```

A function declaration with no body and no `=>` is an external declaration, as in Go. A comma in `Arguments` or a `case` list may be omitted only as §6.13 allows.

## 7. Zero values

`Z(T)` is the canonical zero-value literal for result type `T`:

| `T` | `Z(T)` |
|---|---|
| Pointer, slice, map, channel, function or interface type; `error`; `any` | `nil` |
| Predeclared numeric type, `byte`, `rune`, `uintptr` | `0` |
| `string` | `""` |
| `bool` | `false` |
| Array or struct type literal | `T{}` |
| Named or qualified type | By underlying type: `T{}` for structs and arrays, otherwise the row for its underlying kind |
| Type parameter | None |

Named types are resolved from the package's own declarations or from the export data of the imported package.

If any required `Z(T)` does not exist, the forms that need it (`?`, `? "…"`, `!e`) are invalid in that function, and lifting leaves such code in Go syntax. Lifting also leaves a `return` in Go syntax when its values are zero but not spelled canonically (for example `*new(T)` or `0.0`).

## 8. Canonical lifting order

1. Normalise the Go (§3).
2. Scope elision and implied commas (§6.10–§6.13), on the Go names and types as written.
3. Imports (§6.1).
4. Declarations: drop `func`, receiver inheritance, `!` results, conventional parameters, tag directives.
5. Statements, innermost blocks first: `?` clauses, then `^` and `!` returns, then `in` loops, then `=>` bodies, then inline blocks.
6. Verify step 2 by lowering; on any difference, redo from step 3 without it.
7. Split the channels and apply the layout of §5.

## 9. Outline view

`smol outline` prints the code channel of a package with every function body removed. A declaration without a body reads as its signature. With `-docs`, doc comments are included.

The outline is a read-only view for deciding what to load. It is not a program: it cannot be lowered to the original Go, and L1–L4 do not apply to it.

## 10. Measurements

Real input usage of `gpt-6.1-sol` through Codex for the complete `encoding/json` package, identical in three repetitions. Every request carries 14,469 tokens of harness prompt that no source format can reduce.

| Input | Source tokens | Source reduction | Whole request | Request reduction |
|---|---|---|---|---|
| Go | 32,909 | | 47,378 | |
| Smol 0.1 | 28,898 | 12.2% | 43,367 | 8.5% |
| Smol 0.2 code channel | 16,377 | **50.2%** | 30,846 | 34.9% |
| Outline with doc comments (§9) | 10,616 | 67.7% | 25,085 | 47.1% |
| Outline (§9) | 3,571 | 89.1% | 18,046 | 61.9% |

Three limits on these numbers:

1. The code channel does not contain the comments. They are 30.5% of the Go tokens for this package, so most of the gain over version 0.1 is that split. On the comment-free code alone, version 0.2 removes 26% of the Go tokens (22,162 to 16,377) against 15.6% for version 0.1.
2. A package with fewer comments gains less. The example in §11 has two comment lines and goes from 455 tokens to 250 (45%).
3. Half of the whole request is not reachable by re-encoding the source in this harness: with 14,469 fixed tokens the source would have to shrink by 72%. Only the outline views, which omit function bodies, pass that mark.

## 11. Complete example

Go, 455 tokens:

```go
package users

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
)

// ErrNotFound is returned when a user does not exist.
var ErrNotFound = errors.New("user not found")

type User struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Email     string `json:"email,omitempty"`
}

type Store interface {
	Find(ctx context.Context, id int64) (*User, error)
	List(ctx context.Context) ([]*User, error)
}

type Server struct {
	store Store
}

// Get returns the user with the given id.
func (s *Server) Get(ctx context.Context, id int64) (*User, error) {
	if id <= 0 {
		return nil, ErrNotFound
	}
	u, err := s.store.Find(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("find user %d: %w", id, err)
	}
	return u, nil
}

func (s *Server) Emails(ctx context.Context) ([]string, error) {
	users, err := s.store.List(ctx)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, u := range users {
		if u.Email != "" {
			out = append(out, u.Email)
		}
	}
	return out, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	u, err := s.Get(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if err := json.NewEncoder(w).Encode(u); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (u *User) FullName() string {
	return u.FirstName + " " + u.LastName
}
```

Smol code channel (`users.smol`), 250 tokens:

```
package users
use context http
var ErrNotFound=errors.New("user not found")
type User struct@json{
ID int64
FirstName string
LastName string
Email string `json:"email,omitempty"`
}
type Store interface{
Find(ctx,id int64)*User!
List(ctx)[]*User!
}
type Server struct{
store Store
}
(s*Server)Get(ctx,id int64)*User!{
if id<=0{!ErrNotFound}
u:=store.Find(ctx id)?"find user %d",id
^u
}
.Emails(ctx)[]string!{
users:=store.List(ctx)?
var out[]string for u in users{
if u.Email!=""{out=append(out u.Email)}
}
^out
}
.ServeHTTP(w,r){
id:=strconv.ParseInt(r.URL.Query().Get("id"),10 64)?{
Error(w "bad id" StatusBadRequest)
return
}
u:=Get(r.Context(),id)?{
Error(w err.Error(),StatusNotFound)
return
}
json.NewEncoder(w).Encode(u)?{Error(w err.Error(),StatusInternalServerError)}
}
(u*User)FullName()string=>FirstName+" "+LastName
```

Smol notes channel (`users.smol.notes`):

```
smol-notes 1
@9 var ErrNotFound=errors.New("user not found")
0 c20 "// ErrNotFound is returned when a user does not exist."
0 e1
@16 type User struct@json
0 b
@24 type Store interface
0 b
@7 type Server struct
0 b
@40 (s*Server)Get(ctx,id
0 c20 "// Get returns the user with the given id."
0 e1
@50 .Emails(ctx)[]string!
0 b
@93 .ServeHTTP(w,r)
0 b
@15 (u*User)FullName()string=>
0 b
```

## 12. Conformance

An implementation conforms if, for a corpus that includes at least the Go standard library and the module under translation:

1. `smol check` passes (L1–L3 hold for every package, in the two-channel form).
2. The lowered Go compiles and passes the package's tests.
3. Every form in §6 is covered by a test for the fold, a test for the nearest non-matching Go that must stay unfolded, and a test with a comment inside the template span.
4. Lowering the code channel without notes (L4) is tested for directives and the cgo preamble.

## 13. Open questions

1. **Model accuracy.** Fewer tokens only help if models read and write Smol as accurately as Go. The dense layout, bare members and implied commas all trade legibility for tokens; none has been tested on a coding task.
2. **Notes after edits.** Notes are anchored by position within a declaration. An edit moves that declaration's interior comments above it (§4.1). Anchoring comments to the text of the line they annotate would survive most edits.
3. **Other tokenizers.** All counts are for `o200k_base`. The dense-spacing and implied-break rules should be re-measured for each target model.
4. **Promoted members and types.** §6.11 uses declarations only. With full type information it could cover promoted members and parameters other than the first.
5. **Convention thresholds.** The "two uses" rule of §6.10 and the "three uses" rule of §6.12 are approximations of break-even, chosen without a tokenizer in the tool.
