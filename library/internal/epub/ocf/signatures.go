package ocf

// META-INF/signatures.xml holds the container's digital signatures (§4.2.6.3.6).
// Each Signature names by URL the files it covers, so a rewrite of any of them
// invalidates it — and we hold no key to re-sign with.
//
// Nothing here parses the file, because knowing which entries a signature
// covers would not change the answer. The refusal in the epub package is
// deliberately blunt: a signature is over-refused rather than under-detected,
// which costs an edit on a book that will not turn up in practice — a signed
// EPUB comes from institutional distribution, where it is DRM-encrypted too and
// so already refused by EncryptionInfo.
//
// Reading a signed book is unaffected; only editing one is.
//
// ponytail: the precise version parses the Signature elements and refuses only
// when an entry being replaced is covered. It means committing to xmldsig
// details this package would otherwise never have an opinion about — references
// live in both SignedInfo and Object/Manifest, and transforms change what is
// covered. Write it if a real book is ever refused for nothing.
const SignaturesPath = "META-INF/signatures.xml"
