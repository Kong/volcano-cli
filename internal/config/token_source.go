package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
)

// TokenFDEnv names a file descriptor to read the token from, instead of
// VOLCANO_TOKEN. A process that can read the CLI's environment, such as one a
// coding agent left running next to it, can read VOLCANO_TOKEN; it can't read
// a descriptor the CLI has already read and closed. 0 reads stdin and leaves
// it open; 1 and 2 are refused.
const TokenFDEnv = "VOLCANO_TOKEN_FD" //nolint:gosec // the name of an environment variable, not a credential

// maxTokenFDBytes bounds what is read from the token descriptor.
const maxTokenFDBytes = 16 << 10

// adoptedToken is the token AdoptToken took in, kept in memory only. Token
// returns it ahead of VOLCANO_TOKEN.
var adoptedToken atomic.Pointer[string]

// AdoptToken takes the environment's token into memory, once, at startup,
// before anything reads the configuration.
//
//   - With VOLCANO_TOKEN_FD set, it reads the token's line from that
//     descriptor, closes it (except stdin), and unsets VOLCANO_TOKEN_FD.
//   - With a delegated grant in VOLCANO_TOKEN, it unsets VOLCANO_TOKEN.
//
// Either way, processes the CLI starts (the telemetry flush, git, docker)
// don't inherit the token. Any other token stays in VOLCANO_TOKEN, as before:
// the telemetry flush reads an account token from there.
func AdoptToken() error {
	fdText, fdSet := os.LookupEnv(TokenFDEnv)
	if !fdSet {
		if token := strings.TrimSpace(os.Getenv(envToken)); IsDelegatedGrant(token) {
			adoptedToken.Store(&token)
			return os.Unsetenv(envToken)
		}
		return nil
	}
	if err := os.Unsetenv(TokenFDEnv); err != nil {
		return err
	}
	if strings.TrimSpace(os.Getenv(envToken)) != "" {
		return fmt.Errorf("set %s or %s, not both", TokenFDEnv, envToken)
	}
	fd, err := strconv.Atoi(strings.TrimSpace(fdText))
	if err != nil || fd < 0 {
		return fmt.Errorf("%s must be a file descriptor number", TokenFDEnv)
	}
	token, err := readTokenFD(fd)
	if err != nil {
		return err
	}
	adoptedToken.Store(&token)
	return nil
}

// TokenAdopted reports whether AdoptToken took a token into memory: one from
// VOLCANO_TOKEN_FD, or a delegated grant from VOLCANO_TOKEN.
func TokenAdopted() bool {
	return adoptedToken.Load() != nil
}

// EnvironmentTokenSet reports whether the environment gives a token, in
// VOLCANO_TOKEN or VOLCANO_TOKEN_FD, that Token returns ahead of a saved login.
func EnvironmentTokenSet() bool {
	return environmentToken() != ""
}

// readTokenFD reads the token from descriptor fd.
//
// Stdout and stderr are refused. Stdin is read through os.Stdin and left open:
// closing descriptor 0 would hand the number to the next file the CLI opens,
// and every later stdin read would read that file. Any other descriptor is
// closed after the read.
func readTokenFD(fd int) (string, error) {
	var r io.Reader
	switch fd {
	case 0:
		r = os.Stdin
	case 1, 2:
		return "", fmt.Errorf("%s can't be %d: that is stdout or stderr", TokenFDEnv, fd)
	default:
		file := os.NewFile(uintptr(fd), TokenFDEnv)
		if file == nil {
			return "", fmt.Errorf("%s=%d is not an open file descriptor", TokenFDEnv, fd)
		}
		defer func() { _ = file.Close() }()
		r = file
	}
	return readTokenLine(r, fd)
}

// readTokenLine reads up to the first newline or EOF, so a writer that keeps
// its end open after the token does not hold the CLI up. It reads one byte at
// a time so nothing after the newline is taken from stdin.
func readTokenLine(r io.Reader, fd int) (string, error) {
	var line []byte
	b := make([]byte, 1)
	for {
		n, err := r.Read(b)
		if n == 1 {
			if b[0] == '\n' {
				break
			}
			if len(line) == maxTokenFDBytes {
				return "", fmt.Errorf("the token on %s=%d is over %d bytes", TokenFDEnv, fd, maxTokenFDBytes)
			}
			line = append(line, b[0])
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", fmt.Errorf("failed to read the token from %s=%d: %w", TokenFDEnv, fd, err)
		}
	}
	token := strings.TrimSpace(string(line))
	if token == "" {
		return "", errors.New("no token on " + TokenFDEnv)
	}
	return token, nil
}

// ResetAdoptedTokenForTest forgets the token AdoptToken took in. Test-only:
// each CLI invocation is a fresh process.
func ResetAdoptedTokenForTest() {
	adoptedToken.Store(nil)
}
