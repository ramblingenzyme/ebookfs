# Agent notes

## Comments

### Keep each comment on its declaration

A comment above a declaration is its doc comment. After any edit, check that
none were left behind, duplicated, stacked, detached by a blank line,
displaced by an inserted declaration, made stale, or left naming something
since renamed. After a move, split or rename, check both ends. Three checks
are mechanical:

  - a comment block directly under another is stacked;
  - `grep -c '^\s*//$'` before and after catches a re-wrap that ate paragraph
    breaks;
  - every identifier a comment names should still resolve.

### Paragraph breaks

A comment carrying more than one fact keeps its `//` separator lines. Apply
the delete test per paragraph, and cut a paragraph's separator with it. A
script that joins a block to re-wrap it drops every separator and still
passes gofmt, vet and the tests. Rewrite one block at a time, or treat each
paragraph as its own unit.

### Whether a comment should exist

Default to none. Keep a comment only if you can name the mistake a reader would
make without it. "A reader might wonder" is not a mistake. When unsure, delete.

Run that test before rewording. Rewording a comment that fails it only
shortens it. One pass over internal/testing/util took 38 comment lines to 23
by rewording, and to 9 by applying the test first.

A surviving comment may hold an invariant, a unit or format, what the zero
value means, which side sets a thing, result or call order, concurrency
safety, a spec reference, or a choice between equally plausible options. This
is not a checklist. Every function has a call order, and that alone is no
reason to comment.

Not for history, conventions stated in this file, or where the code lives.
The one exception: a regression test may name the defect it guards.

Templates write empty comments. `// Name does X` and "…rather than X, which
would Y" are always available, so reach for a form only after the comment has
passed the test. Open a doc comment on its identifier. Keep field comments to
one line. Put reasoning that spans packages in docs/DECISIONS.md and point to
it.

Test functions skip the name, since it sits on the next line. Open on the
fact:

	// The inverse of canonicalDir's " (id)" suffix.

Test helpers rarely keep a doc. Keep one only for a fact from outside the
body, such as an import rule or the spec clause a fixture satisfies.

### Say it once

A rationale lives in the function that enforces it, and other sites point
there ("package ncx says why"). A one-line pass-through points at the doc of
whatever it delegates to rather than copying it.

### Cite, don't paraphrase

Name the section (`§5.5.3.1.2`, `D.3.7`, `OPF 2.0 §2.6`) and quote at most
one clause, only where the exact wording is what the code turns on. In spec
tests, never cut the section number or the quote. Where a test asserts more
than the spec requires, say which half is ours.

### How a comment reads

  - Present tense, third person or imperative. No "I", rarely "we".
  - Active voice, with the actor as subject and its verb beside it.
  - One idea per sentence. An em dash, a colon or a third comma usually marks
    two sentences.
  - Name the concrete consequence, not the importance: "forces a full reindex
    on every startup", not "this matters".
  - Back a magnitude claim with a number, or cut it.
  - Plain connectives: also, though, but, since, so.
  - No "not X, but Y", no mirrored "one … the other".
  - No hedging, "note that", "simply", or decayed words like robust, seamless
    or leverage.
  - No recaps, aphorisms, or closing flourishes.

Turn history into a standing property:

	// Nothing in CI builds that tag, which is how it sat uncompilable for
	// four commits after library.Open changed shape.

	// Nothing in CI builds this tag, so a change to library.Open breaks
	// this file without failing any build.

## Test files

  - `foo_test.go` tests `foo.go` and is white-box (`package foo`).
  - `foo_ext_test.go` is black-box (`package foo_test`) and covers the contract
    other packages rely on.
  - Tests spanning several source files go in `_scenario_test.go`, with a
    header saying what they pin. Never name a test file for a quality or a
    feature.
  - `helpers_test.go` and `helpers_ext_test.go` hold shared helpers and no
    tests. The package rule still applies.

## Working

  - **Sweeps:** do one package, then stop, report what changed, and wait for
    review before the next.
  - **Before compressing:** check whether a premature abstraction should be
    undone first. A method that reads one field of its receiver usually wants
    to be a free function taking that value.
  - **Before calling something a bug:** grep docs/DECISIONS.md. Several
    decisions choose strictness over availability and look like defects. If a
    decision covers it, honour it or raise the conflict and ask. Improving the
    diagnostics of a deliberate failure is usually welcome.
  - **Planning docs:** keep TODO.md and docs/DECISIONS.md at the design level.
    No file:line references and no internal Go symbols. Public names (9P paths,
    config fields, a proposed interface) are fine.
