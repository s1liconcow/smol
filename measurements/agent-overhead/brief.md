Smol is Go with these changes. Top-level functions and methods omit `func`;
function literals/types keep it. A method starting `.` inherits the preceding
receiver. A result suffix `!` appends error: `F() T!` means (T,error).
In fallible functions `^x` means `return x,nil`; `!e` returns zero values plus e
(only with at least two results). `x:=f()?` appends err to the assignment and
returns zero values plus err on error; `? {body}` supplies a custom handler.
`for v in xs` iterates values; `for k,v in xs` iterates keys and values.
`=> expr` is a body containing `return expr`.
`use fmt reflect` opens package names; free exports infer their qualifier.
Bare fields/methods refer to the receiver or first local-type parameter unless
shadowed. `let c *Context` gives bare parameter/receiver c that type.
Built-in parameter conventions are ctx=context.Context, w=http.ResponseWriter,
r=*http.Request, t=*testing.T. Standard imports and smol.mod imports are inferred.
`struct @json` supplies snake_case field tags; `~` suppresses one.
Adjacent identifier/literal call arguments imply commas. Other lists keep commas.
Everything else is Go. Explicit imports, types, selectors, returns, braces and
commas work. Use explicit receivers for new methods and normal Go statements
when unsure. Whitespace is accepted; do not invent shorthand.
