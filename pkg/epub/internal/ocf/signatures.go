package ocf

// SignaturesPath is the container's digital signatures file (§4.2.6.3.6).
// Nothing here parses it: the epub package refuses an edit on its presence
// alone, for the reasons in docs/DECISIONS.md #23.
const SignaturesPath = "META-INF/signatures.xml"
