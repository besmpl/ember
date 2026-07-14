package fixture

// import "C" and plugin.Open("not-real") are only text.
const sourceText = `os/exec, syscall.Mmap, //go:linkname fake`

func commentsAndStringsFixture() string { return sourceText }
