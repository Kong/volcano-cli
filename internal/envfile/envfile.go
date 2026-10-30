// Package envfile parses .env files into key/value pairs.
package envfile

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/joho/godotenv"

	"github.com/Kong/volcano-cli/internal/sensitive"
)

// ErrNotFound is returned when no path is given and neither default location
// holds an env file.
var ErrNotFound = errors.New("no volcano.env file found.\ncreate volcano/volcano.env or use --file to specify a path")

// File is a loaded Volcano env file.
type File struct {
	Path      string
	Variables map[string]string
}

func (f *File) envVars() []string {
	keys := make([]string, 0, len(f.Variables))
	for key := range f.Variables {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	envs := make([]string, 0, len(keys))
	for _, key := range keys {
		envs = append(envs, key+"="+f.Variables[key])
	}
	return envs
}

// Load resolves and reads the selected Volcano env file.
func Load(fileArg string) (*File, error) {
	path, err := ResolvePath(fileArg)
	if err != nil {
		return nil, err
	}
	return loadPath(path)
}

// Stdin is the file argument that reads the env file from standard input.
const Stdin = "-"

// StdinName is how messages name an env file read from standard input.
const StdinName = "standard input"

func loadPath(path string) (*File, error) {
	variables, err := godotenv.Read(path)
	return parsed(path, variables, err)
}

// Read parses an env file from r, which name describes in messages. The values
// stay in memory: nothing is written to disk.
//
// Unlike a file on disk, the input is usually a secret manager's output, and
// stderr is often a CI log, so a parse error leaves out the parser's message,
// which quotes the text it failed on.
func Read(name string, r io.Reader) (*File, error) {
	variables, err := godotenv.Parse(r)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: it is not a valid env file (the parser's message is not shown because it quotes the input)", name)
	}
	return parsed(name, variables, nil)
}

func parsed(name string, variables map[string]string, err error) (*File, error) {
	if err != nil {
		// The parser quotes the file's text, which holds the variable values.
		return nil, fmt.Errorf("failed to read %s: %w", name, sensitive.Wrap(err))
	}
	for variable := range variables {
		if variable == "" {
			return nil, fmt.Errorf("failed to read %s: empty variable name", name)
		}
	}

	return &File{Path: name, Variables: variables}, nil
}

func loadFirstExisting(candidates ...string) (*File, bool, error) {
	for _, candidate := range candidates {
		found, err := fileExists(candidate)
		if err != nil {
			return nil, false, err
		}
		if !found {
			continue
		}
		file, err := loadPath(candidate)
		if err != nil {
			return nil, false, err
		}
		return file, true, nil
	}
	return nil, false, nil
}

// LoadFirstEnvVars reads the first existing env file from candidates and
// returns its variables as sorted KEY=value entries. If no candidate exists, it
// returns nil and no error.
func LoadFirstEnvVars(candidates ...string) ([]string, error) {
	file, found, err := loadFirstExisting(candidates...)
	if err != nil || !found {
		return nil, err
	}
	return file.envVars(), nil
}

// ResolvePath returns the env file Load would read. An explicit path must
// exist; otherwise it looks for volcano/volcano.env and ./volcano.env, returns
// ErrNotFound when neither exists, and an error when both do.
func ResolvePath(fileArg string) (string, error) {
	if fileArg != "" {
		found, err := fileExists(fileArg)
		if err != nil {
			return "", err
		}
		if !found {
			return "", fmt.Errorf("specified file not found: %s", fileArg)
		}
		return fileArg, nil
	}

	candidates := []string{
		filepath.Join("volcano", "volcano.env"),
		"volcano.env",
	}
	found := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		ok, err := fileExists(candidate)
		if err != nil {
			return "", err
		}
		if ok {
			found = append(found, candidate)
		}
	}

	if len(found) > 1 {
		return "", fmt.Errorf("found multiple volcano.env files: %s.\nplease keep only one volcano.env file, or specify explicitly with --file", strings.Join(found, ", "))
	}
	if len(found) == 1 {
		return found[0], nil
	}

	return "", ErrNotFound
}

func fileExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return false, fmt.Errorf("failed to access %s: %w", path, err)
}
