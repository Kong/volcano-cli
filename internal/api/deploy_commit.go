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

// writeCommitField adds the commit as the JSON-encoded string the frontend
// upload takes.
func writeCommitField(writer *multipart.Writer, commit *DeployCommit) error {
	if commit == nil {
		return nil
	}
	encoded, err := json.Marshal(commit)
	if err != nil {
		return fmt.Errorf("failed to encode commit field: %w", err)
	}
	if err := writer.WriteField("commit", string(encoded)); err != nil {
		return fmt.Errorf("failed to write commit field: %w", err)
	}
	return nil
}
