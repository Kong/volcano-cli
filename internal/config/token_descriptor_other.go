//go:build !unix

package config

// descriptorsByNumber is false where os.NewFile takes a handle, such as a
// Windows HANDLE, rather than a descriptor number: VOLCANO_TOKEN_FD can only be
// 0 (stdin) there.
const descriptorsByNumber = false
