package enc

// Instructions is the exact stable prompt text from SPEC_ENC §7. No examples
// or additional shorthand rules are appended by the tool.
func Instructions(profile Profile) string {
	const e1 = `Reply with a patch only: no prose, no code fence.
@@ path — the file; for a new file its whole contents follow.
== A — replace line A with the lines that follow.
-- A — delete line A.
++ A — insert the lines that follow after line A.
^^ A — insert them before line A.
A line ".." after ==, -- or ++ extends A to the whole block that line A opens; ".. B" extends it through line B.
A and B are the start of an existing line, long enough to match no other line. "S @ A" looks for A only inside the block opened by line S.
Write the new lines as ordinary Go, without indentation and without import declarations.
`
	if profile == E0 {
		return ""
	}
	if profile == E2 {
		return e1 + "In new lines you may also write: \"x := f()?\" for \"x, err := f()\" followed by returning zero values and err when err != nil; \"f()?\" likewise when f returns only an error.\n"
	}
	return e1
}
