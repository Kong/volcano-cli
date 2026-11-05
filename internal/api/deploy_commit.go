package api

import (
	"encoding/json"
	"fmt"
	"mime/multipart"
)

// DeployCommit is the commit a frontend upload's source came from: the
// GitHub repository as owner/name, the full commit ID, whether the uploaded
// files differ from that commit, and where the uploaded directory sits in the
// repository. A nil commit sends nothing.
type DeployCommit struct {
	Repository string `json:"repository"`
	SHA        string `json:"sha"`
	Dirty      bool   `json:"dirty"`
	// Root is the uploaded directory's slash-separated path from the
	// repository root, "" for the root itself. Each archive entry is the
	// file at Root/<entry> in the commit.
	Root string `json:"root"`
}

// FunctionCommit is the commit a function upload's source came from: the
// GitHub repository as owner/name, the full commit ID, and whether the
// function's files differ from that commit. A function upload takes no root.
// A nil commit sends nothing.
type FunctionCommit struct {
	Repository string `json:"repository"`
	SHA        string `json:"sha"`
	Dirty      bool   `json:"dirty"`
}

// writeCommitField adds the commit as the JSON-encoded string the frontend
// upload takes.
func writeCommitField(writer *multipart.Writer, commit *DeployCommit) error {
	if commit == nil {
		return nil
	}
	return writeJSONCommitField(writer, commit)
}

// writeFunctionCommitField adds the commit as the JSON-encoded string the
// function and durable function uploads take.
func writeFunctionCommitField(writer *multipart.Writer, commit *FunctionCommit) error {
	if commit == nil {
		return nil
	}
	return writeJSONCommitField(writer, commit)
}

func writeJSONCommitField(writer *multipart.Writer, commit any) error {
	encoded, err := json.Marshal(commit)
	if err != nil {
		return fmt.Errorf("failed to encode commit field: %w", err)
	}
	if err := writer.WriteField("commit", string(encoded)); err != nil {
		return fmt.Errorf("failed to write commit field: %w", err)
	}
	return nil
}
