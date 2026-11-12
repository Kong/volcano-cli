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
// a descriptor the CLI has already read and closed. 0 reads stdin.
const TokenFDEnv = "VOLCANO_TOKEN_FD" //nolint:gosec // the name of an environment variable, not a credential

// maxTokenFDBytes bounds what is read from the token descriptor.
const maxTokenFDBytes = 16 << 10

// adoptedToken is the token AdoptToken took in, kept in memory only. Token
// returns it ahead of VOLCANO_TOKEN.
var adoptedToken atomic.Pointer[string]

// AdoptToken takes the environment's token into memory, once, at startup,
// before anything reads the configuration.
//
//   - With VOLCANO_TOKEN_FD set, it reads the token from that descriptor,
//     closes it, and unsets VOLCANO_TOKEN_FD.
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

// readTokenFD reads the token from descriptor fd and closes it.
func readTokenFD(fd int) (string, error) {
	file := os.NewFile(uintptr(fd), TokenFDEnv)
	if file == nil {
		return "", fmt.Errorf("%s=%d is not an open file descriptor", TokenFDEnv, fd)
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, maxTokenFDBytes+1))
	if err != nil {
		return "", fmt.Errorf("failed to read the token from %s=%d: %w", TokenFDEnv, fd, err)
	}
	if len(data) > maxTokenFDBytes {
		return "", fmt.Errorf("the token on %s=%d is over %d bytes", TokenFDEnv, fd, maxTokenFDBytes)
	}
	token := strings.TrimSpace(string(data))
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
