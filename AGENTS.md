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

History has one legitimate form. A regression test may name the defect it
guards, because that is why the test exists and git will not surface it to
someone reading the file. "It used to be a path lookup, which missed a book
credited in either order" earns its place. "This broke in commit abc123" does
not.

Default to one sentence opening with the identifier. A field comment is one
line, and only when name and type don't already say it.

Length follows from how many facts survive the test below, not from a cap. A
comment carrying four consequences is four facts long, and the spec tests in
library/internal/epub are right to run past ten lines: each names a consequence,
a spec ambiguity, or a deliberate narrowing that the code cannot state. Padding
is wrong at any length, and a cap would cut the wrong end first. Reasoning that
spans packages belongs in DECISIONS.md with a pointer from the code.

In those spec tests the section number and the verbatim quote are the part that
must survive. They let a reader check the assertion against the spec without
leaving the file. Cut the prose around them, never them.

The test: delete it. If a competent reader recovers the fact from the code,
leave it deleted.

## How a comment reads

Present tense, describing the code as it stands. Third person or imperative;
not "I", and rarely "we". State the consequence concretely instead of calling
something important: "recording one would force a full reindex on every
startup" beats "this matters".

Explaining a choice means naming the alternative that was rejected and why it
lost. No hedging, no asides, nothing addressed to one reader at one moment.

Name the thing doing the acting, and keep subject and verb together. An
abstract summary noun standing in for a subject reads worse than a plain
sentence and is usually longer. Write "if a rebuild does not record what it
found, the next startup rebuilds again", not "the failure these share is a
rebuild that does not record what it saw". This matters more than length: a
short comment in that register is still hard to follow.

Mechanics that keep it plain:

  - Active voice. Passive only when the actor is genuinely unknown.
  - One idea per sentence. A sentence reaching for an em dash is usually two
    sentences; prefer the period.
  - No "not X, but Y", and no mirrored "one ... the other" comparisons. Both
    read as structure where a fact belongs. "The closing half of" and "the
    other direction from" are the same tic.
  - A magnitude claim carries a number or gets cut. "Every startup" beats
    "often"; "five minutes" beats "a long time".
  - Plain connectives: also, though, but, since, so.
  - No throat-clearing. Delete "note that", "it is worth noting", "keep in
    mind". Start with the fact.
  - No decayed words: robust, seamless, dynamic, innovative, leverage as a
    verb. They carry no information.
  - No closing recap. If the last sentence restates the comment, delete it.

Turn history into a standing property:

	// Nothing in CI builds that tag, which is how it sat uncompilable for
	// four commits after library.Open changed shape.

	// Nothing in CI builds this tag, so a change to library.Open breaks
	// this file without failing any build.

The first is true until someone adds the tag to CI. The second stays true and
tells a reader what to watch for.

## Paragraph breaks, and editing comments with a script

A comment carrying more than one fact keeps its `//` separator lines. Four
short paragraphs scan; one twelve-line wall does not, however good the
sentences are. When you shorten a comment, shorten the paragraphs and keep the
breaks.

This is the thing a script gets wrong. Joining a comment block to re-wrap it
silently drops every `//` separator inside it, and the result passes gofmt, vet
and the tests while reading worse than what it replaced. Rewrite comments one
block at a time with the paragraph structure in hand, or make the script treat
each paragraph as its own unit. Check `grep -c '^\s*//$'` before and after: that
count must not fall.
