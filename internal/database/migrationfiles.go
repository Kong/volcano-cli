package database

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// MigrationsDir is where a project keeps its SQL migration files.
const MigrationsDir = "volcano/migrations"

// MigrationFile is one migration file. Path is its path under MigrationsDir
// with / separators: a file name, or <database>/<file name>.
type MigrationFile struct {
	Path string
	SQL  string
}

// Name is the file name the database's migration history records.
func (f MigrationFile) Name() string {
	return path.Base(f.Path)
}

// Checksum is the checksum the migration history records for the file: its
// SHA-256 with a leading byte order mark removed and CRLF line endings
// converted to LF, so checkouts on different platforms agree.
func (f MigrationFile) Checksum() string {
	normalized := strings.ReplaceAll(strings.TrimPrefix(f.SQL, "\uFEFF"), "\r\n", "\n")
	sum := sha256.Sum256([]byte(normalized))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Migrations are a project's migration files. Either every file sits in
// MigrationsDir itself, for a project with one database, or every file sits
// one directory down, in a directory named for its database. Git deploy reads
// the directory the same way, so a layout one refuses the other refuses too.
type Migrations struct {
	files  []MigrationFile
	nested bool
}

// ReadMigrations reads the .sql files under dir, skipping entries whose names
// start with "." or "_" and everything beneath them. A missing dir holds no
// migrations. The limits on how many files a run takes and how large they may
// be are the API's to enforce.
func ReadMigrations(dir string) (Migrations, error) {
	info, err := os.Stat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return Migrations{}, nil
	}
	if err != nil {
		return Migrations{}, err
	}
	if !info.IsDir() {
		return Migrations{}, fmt.Errorf("%s is not a directory", dir)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return Migrations{}, err
	}
	defer func() { _ = root.Close() }()

	var files []MigrationFile
	err = fs.WalkDir(root.FS(), ".", func(rel string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if rel == "." {
			return nil
		}
		shown := path.Join(filepath.ToSlash(dir), rel)
		if strings.HasPrefix(d.Name(), ".") || strings.HasPrefix(d.Name(), "_") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		// Git deploy does not follow links, so a migration reached through
		// one would apply from here and be missing there.
		if d.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("%s is a symlink, which Git deploy does not follow; replace it with the file or directory it points to", shown)
		}
		if d.IsDir() {
			return nil
		}
		if !strings.EqualFold(path.Ext(d.Name()), ".sql") {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("%s is not a regular file", shown)
		}
		if strings.Count(rel, "/") > 1 {
			return fmt.Errorf("%s is nested too deeply; place migrations in %s or one directory below it", shown, MigrationsDir)
		}
		sql, err := root.ReadFile(rel)
		if err != nil {
			return err
		}
		files = append(files, MigrationFile{Path: rel, SQL: string(sql)})
		return nil
	})
	if err != nil {
		return Migrations{}, err
	}
	slices.SortFunc(files, func(a, b MigrationFile) int { return strings.Compare(a.Path, b.Path) })

	var flat, nested *MigrationFile
	for i := range files {
		if strings.Contains(files[i].Path, "/") {
			if nested == nil {
				nested = &files[i]
			}
		} else if flat == nil {
			flat = &files[i]
		}
	}
	if flat != nil && nested != nil {
		return Migrations{}, fmt.Errorf("%s mixes top-level files, such as %s, with database directories, such as %s/; "+
			"put every file in %s/<database>/, or start the name of a directory to skip with _",
			MigrationsDir, flat.Path, path.Dir(nested.Path), MigrationsDir)
	}
	return Migrations{files: files, nested: nested != nil}, nil
}

// Empty reports whether there are no migration files.
func (m Migrations) Empty() bool {
	return len(m.files) == 0
}

// Nested reports whether the files sit in per-database directories.
func (m Migrations) Nested() bool {
	return m.nested
}

// Databases returns the databases the directories name, in name order. It is
// empty when the files sit in MigrationsDir itself.
func (m Migrations) Databases() []string {
	if !m.nested {
		return nil
	}
	names := map[string]bool{}
	for _, file := range m.files {
		names[path.Dir(file.Path)] = true
	}
	return slices.Sorted(maps.Keys(names))
}

// For returns the files that belong to database, in the order they run: every
// file when they sit in MigrationsDir itself, or the files in the directory
// named for database.
func (m Migrations) For(database string) []MigrationFile {
	if !m.nested {
		return slices.Clone(m.files)
	}
	var files []MigrationFile
	for _, file := range m.files {
		if path.Dir(file.Path) == database {
			files = append(files, file)
		}
	}
	return files
}

// Location names where database's files live, for messages.
func (m Migrations) Location(database string) string {
	if m.nested {
		return MigrationsDir + "/" + database
	}
	return MigrationsDir
}
