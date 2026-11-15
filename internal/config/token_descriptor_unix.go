//go:build unix

package config

// descriptorsByNumber reports whether os.NewFile takes a descriptor number, so
// VOLCANO_TOKEN_FD can name a descriptor from 3 up.
const descriptorsByNumber = true
