package api

import (
	"encoding/json"
	"fmt"
	"mime/multipart"
)

// DeployCommit is the commit an upload's source came from: the GitHub
// repository as owner/name, the full commit ID, and whether the working tree
// had uncommitted changes. A nil commit sends nothing.
type DeployCommit struct {
	Repository string `json:"repository"`
	SHA        string `json:"sha"`
	Dirty      bool   `json:"dirty"`
}

// writeCommitField adds the commit as the JSON-encoded string the single
// function, durable function and frontend uploads take. The batch upload
// nests it in its manifest instead.
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
