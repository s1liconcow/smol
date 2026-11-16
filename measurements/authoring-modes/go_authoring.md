Match old strings exactly as supplied. Write newly authored code in ordinary Go
syntax; the lowerer accepts it inside Smol files. Keep all commas, explicit func
declarations, parameter types, receiver/package qualifiers, braces and return
statements. Existing Smol copied from the original can stay unchanged. Keep the
provided .smol file paths. The formatter handles compression afterwards.
