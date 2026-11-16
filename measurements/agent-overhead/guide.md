Smol v0.2 is Go surface syntax. Everything not listed below keeps Go semantics.
Write code directly in the supplied representation. Explicit Go expressions,
types, returns, imports and control flow are accepted; follow existing compact
forms when editing. Do not invent shorthand. Formatting is handled afterwards.

Declarations: omit `func` only on top-level functions/methods; function literals
and function types keep `func`. `(s *Server) Get(x int) int { ... }` is a method.
A subsequent `.Close()! { ... }` inherits the nearest preceding method receiver.
When adding a method, an explicit receiver is safest. `F()!` returns error;
`F() T!` returns (T,error); `F()(T,U)!` returns (T,U,error). Named results keep
Go syntax. The receiver and parameters may use conventional bare names:
`ctx` = context.Context, `w` = http.ResponseWriter, `r` = *http.Request,
`t` = *testing.T. A `let c *Context, v reflect.Value` line after `package`/`use`
adds file-local parameter conventions. `(c)` then means `(c *Context)`.
Conventional names never inherit a following parameter's type. Write explicit
types to avoid ambiguity, or `\name` to suppress a convention on a bare name.

Imports: standard-library qualifiers such as `fmt.Errorf` infer imports.
Third-party qualifiers resolve through the supplied smol.mod import table.
You can always use explicit Go import declarations. `import (...; ...)` uses
`...` as a placeholder for inferred imports, not as a real package.
`use fmt reflect` directly after the package clause opens those packages:
free exported names such as `Errorf` or `Value` expand to the uniquely matching
package member. Scope declarations and built-ins take precedence.

Subject elision: in a method, bare fields/methods can refer to the receiver.
In a function whose first parameter has a local package type, they can refer to
that parameter. This includes direct fields, embedded field names and declared
methods, but not promoted fields. A local variable shadows the bare member.
Explicit `receiver.Field` is always available. Preserve explicit qualifiers
when a name is shadowed; do not introduce a local that accidentally captures
existing bare member uses. Qualified composite literal keys stay qualified.

Error handling: `x := f()?` expands to `x,err := f(); if err != nil {
return ZERO,err }`. `f()?` expands to `if err := f(); err != nil {
return ZERO,err }`. Here ZERO supplies every earlier result's zero value.
`x := f()? "read %s", path` wraps with fmt.Errorf("read %s: %w",path,err).
`x := f()? { ... }` uses an explicit handler with err in scope. If the left
side is already `err :=` or `err =`, no extra err is appended. `?` attaches
to a statement; it is never an expression operator. In a fallible function,
`^x` means `return x,nil`; bare `^` means `return nil` for an error-only result.
`!e` means `return ZERO,e` only when there are at least two results. With just
an error result use `return e`. Keep normal Go returns if zero types are unclear.

Loops: `for v in xs` means `for _,v := range xs`; `for k,v in xs` means
`for k,v := range xs`. For indices only keep `for k := range xs`.
`F(x int) int => x+1` is a function with one return statement. The arrow is
optional for authoring: braces and explicit `return` work. Keep braces around
a function literal before a call, selector, index or binary operator.

Tags: `type T struct @json { UserID int; Cache int ~ }` gives UserID the tag
`json:"user_id"`; `~` suppresses the implicit tag. Explicit tags override it.
Multiple directives add multiple keys. Unexported fields get no implicit tags.

Layout: braces and Go semicolon rules remain. Whitespace and indentation are
accepted. Adjacent simple identifier/literal arguments imply commas inside
calls and case lists: `append(dst b)` means `append(dst,b)`. Keep normal commas
in signatures, assignments, returns and composite literals. Existing dense
lines can contain an implied newline before a statement keyword (`x:=1 if ...`).
Prefer actual newlines in new code. Comments live in a separate notes channel;
compiler directives remain in code. Do not edit notes for this experiment.
