# Agent notes

## A comment belongs to its declaration

A comment sitting above a declaration is that declaration's doc comment, not
loose text between functions. This is where comments rot, so it is the first
thing to check after an edit, ahead of anything about how they read.

The ways one comes off its declaration, each seen in this repo:

  - Left behind. Code moved or was deleted and the comment stayed, describing
    whatever landed below it.
  - Duplicated. A split copied the comment into both halves, and the two
    drift apart.
  - Stacked. A rewrite was prepended and the original never deleted, so one
    declaration carries two doc comments.
  - Detached. A blank line between a comment and its field leaves it
    documenting nothing.
  - Displaced. A var or const inserted under a doc comment takes it, and
    godoc attributes the paragraph to the wrong declaration.
  - Stale. The code changed and the comment still asserts the old behaviour.
    Worse than no comment, because a reader trusts it.
  - Renamed. The comment names an identifier, package or file that has since
    been renamed, so it points at nothing. Three comments in
    internal/testing/util still said `testutil` after the package became
    `util`.

After a move, split or rename, check both ends. Three of these are mechanical:
a comment block immediately following another comment block is stacked, the
separator count below catches a re-wrap that ate the paragraph breaks, and
every identifier a comment names should still resolve, which grep answers.

## Paragraph breaks, and editing comments with a script

A comment carrying more than one fact keeps its `//` separator lines. Four
short paragraphs scan; one twelve-line wall does not, however good the
sentences are.

Delete-it applies per paragraph, not just per comment. A paragraph whose fact
the code now gives goes, and its separator goes with it.

This is the thing a script gets wrong. Joining a comment block to re-wrap it
silently drops every `//` separator inside it, and the result passes gofmt, vet
and the tests while reading worse than what it replaced. Rewrite comments one
block at a time with the paragraph structure in hand, or make the script treat
each paragraph as its own unit. `grep -c '^\s*//$'` before and after catches
that: a count that fell for a paragraph you did not decide to cut is a re-wrap
that ate a break.

## Whether a comment should exist

The test: delete it. If a competent reader recovers the fact from the code,
leave it deleted.

Run the test first, then decide whether what survives needs rewriting.
Shortening a comment that fails the test gives a shorter comment that still
fails it, and the trimming makes the result look considered, so the next
reader leaves it alone. One pass over internal/testing/util took 38 comment
lines to 23 by rewording, and to 9 by applying the test first.

What survives is what the code can't say: an invariant between fields, a unit
or format, what the zero value means, which side sets a thing and which reads
it, result ordering, call order, concurrency safety. A spec reference with its
section number. A choice where another option looked equally right, so nobody
later "fixes" it back.

Not for: restating the name or the line below it; history, which git already
holds; conventions that live in this file; the author's reasoning about where
to put the code.

History has one legitimate form. A regression test may name the defect it
guards, because that is why the test exists and git will not surface it to
someone reading the file. "It used to be a path lookup, which missed a book
credited in either order" earns its place. "This broke in commit abc123" does
not.

Length follows from how many facts survive the test, not from a cap. A comment
carrying four consequences is four facts long, and the spec tests in epub are
right to run past ten lines: each names a consequence, a spec ambiguity, or a
deliberate narrowing that the code cannot state. Padding is wrong at any
length, and a cap would cut the wrong end first. Reasoning that spans packages
belongs in DECISIONS.md with a pointer from the code.

Default to one sentence opening with the identifier, and watch that the
template does not write the comment for you: `// Name does X` is always
available, which makes an empty one easy to produce. A field comment is one
line, and only when name and type don't already say it.

Test functions are the exception. Their names are long and describe the claim
already, godoc never surfaces them, and the name sits on the very next line, so
repeating it is noise. Open on the fact instead:

	// TestIDFromPath pins the inverse of canonicalDir's " (id)" suffix.

	// The inverse of canonicalDir's " (id)" suffix.

Test helpers rarely keep a doc at all. A helper's body runs one to eight lines
with no branches, so the name and the body carry everything: `MakeBook`,
`WrapBook`, `Fixed` and `NewTestFS` each lost a doc that restated a one-line
body. What survives comes from outside the helper, such as an import rule the
compiler does not enforce, or the spec clause a fixture satisfies.

## Say it once

A rule belongs in one place, usually the function that enforces it. Other sites
point at it ("package ncx says why"). Repeating a rationale across a package
doc, a method, its caller and its test is four places to update and three
places to go stale.

A one-line pass-through is the common trap: it invites a copy of the doc
comment of whatever it delegates to. Point at that instead.

## Cite, don't paraphrase

Name the section, `§5.5.3.1.2`, `D.3.7`, `OPF 2.0 §2.6`, and quote at most one
clause, only where the exact wording is what the code turns on. Do not restate
the spec's argument in your own words; the reader can open `specs/`.

Where a test asserts something the specs do not require, say which half is
ours. The existing spec tests do this and it is worth keeping.

In those spec tests the section number and the verbatim quote are the part that
must survive. They let a reader check the assertion against the spec without
leaving the file. Cut the prose around them, never them.

## How a comment reads

Present tense, describing the code as it stands. Third person or imperative;
not "I", and rarely "we". State the consequence concretely instead of calling
something important: "recording one would force a full reindex on every
startup" beats "this matters".

Explaining a choice means naming the alternative that was rejected and why it
lost, in a clause: "…rather than X, which would Y" is the whole form.

Name the thing doing the acting and keep subject and verb together. Write "if a
rebuild does not record what it found, the next startup rebuilds again", not
"the failure these share is a rebuild that does not record what it saw". An
abstract summary noun standing in for a subject reads worse and is usually
longer.

Mechanics that keep it plain:

  - Active voice. Passive only when the actor is genuinely unknown.
  - One idea per sentence. A sentence reaching for an em dash, a colon, or a
    third comma is usually two sentences; prefer the period. Banning only the
    em dash moves the same construction onto the colon, which is how this
    repo ended up with 554 of them.
  - No "not X, but Y", and no mirrored "one ... the other" comparisons. Both
    read as structure where a fact belongs. "The closing half of" and "the
    other direction from" are the same tic.
  - A magnitude claim carries a number or gets cut. "Every startup" beats
    "often"; "five minutes" beats "a long time".
  - Plain connectives: also, though, but, since, so.
  - No throat-clearing, hedging, or asides addressed to one reader at one
    moment. Delete "note that", "it is worth noting", "keep in mind", "simply".
  - No decayed words: robust, seamless, dynamic, innovative, leverage as a
    verb. They carry no information.
  - No closing recap, and no flourishes. Cut aphorisms and the closing
    sentence that restates the paragraph with feeling. These are all wrong:
    "…is the whole safety rule of this package"; "…which beats writing a title
    into one of the two places that claim to hold it".

Turn history into a standing property:

	// Nothing in CI builds that tag, which is how it sat uncompilable for
	// four commits after library.Open changed shape.

	// Nothing in CI builds this tag, so a change to library.Open breaks
	// this file without failing any build.

The first is true until someone adds the tag to CI. The second stays true and
tells a reader what to watch for.

## Test files pair with source files

`foo_test.go` holds the tests for `foo.go` and is white-box (`package foo`).
`foo_ext_test.go` is black-box (`package foo_test`) and describes the contract
other packages rely on. Filename and package always agree: a bare name means
white-box, the `_ext_` marker means black-box, no exceptions.

Tests spanning several source files cannot pair with one, so they take
`_scenario_test.go` and a file header saying what they pin. Never name a test
file for a quality or a feature. `library_err_test.go` held the only tests for
four different source files and nothing in the name said so.

`helpers_test.go` and `helpers_ext_test.go` hold what a package's test files
share and no tests of their own, so they pair with the suite rather than with a
source file. The package rule still governs, and a package carries both when
its white-box and black-box suites each need helpers.

## `ponytail:` marks a deliberate ceiling

A shortcut taken knowingly carries a `ponytail:` comment naming the limit and
what would justify lifting it, in two lines.

	// ponytail: rescans per call, O(n²) over a document holding tens of elements.
	// Thread a set through the callers only if a profile ever says to.
