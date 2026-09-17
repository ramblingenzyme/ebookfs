# Agent notes

## Comments belong to a declaration

A comment sitting above a declaration is that declaration's doc comment, not
loose text between functions. When you move, split, or rename code, the comment
moves with it. After any such change, check both ends: a comment duplicated into
two files, and a comment left behind pointing at code that is no longer below it.

## Test files pair with source files

`foo_test.go` holds the tests for `foo.go` and is white-box (`package foo`).
`foo_ext_test.go` is black-box (`package foo_test`) and describes the contract
other packages rely on. Filename and package always agree: a bare name means
white-box, the `_ext_` marker means black-box, no exceptions.

Tests spanning several source files cannot pair with one, so they take
`_scenario_test.go` and a file header saying what they pin. Never name a test
file for a quality or a feature. `library_err_test.go` held the only tests for
four different source files and nothing in the name said so.

## What a comment is for, and how terse

The code says what it does. A comment says what the code can't: an invariant
between fields, a unit or format, what the zero value means, which side sets a
thing and which reads it, result ordering, call order, concurrency safety. A
spec reference with its section number. A choice where another option looked
equally right, so nobody later "fixes" it back.

Not for: restating the name or the line below it; history, which git already
holds; conventions that live in this file; the author's reasoning about where
to put the code.

A doc comment is one sentence opening with the identifier, plus at most two
more lines if a caller needs something the signature doesn't give. A field
comment is one line, and only when name and type don't already say it. Past
that, the reasoning belongs in DECISIONS.md with a pointer from the code.

The test: delete it. If a competent reader recovers the fact from the code,
leave it deleted.

## How a comment reads

Present tense, describing the code as it stands. Third person or imperative;
not "I", and rarely "we". State the consequence concretely instead of calling
something important: "recording one would force a full reindex on every
startup" beats "this matters".

Explaining a choice means naming the alternative that was rejected and why it
lost. No hedging, no asides, nothing addressed to one reader at one moment.

Turn history into a standing property:

	// Nothing in CI builds that tag, which is how it sat uncompilable for
	// four commits after library.Open changed shape.

	// Nothing in CI builds this tag, so a change to library.Open breaks
	// this file without failing any build.

The first is true until someone adds the tag to CI. The second stays true and
tells a reader what to watch for.
