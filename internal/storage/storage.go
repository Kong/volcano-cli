// Package storage performs authenticated Volcano storage workflows.
package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	clisession "github.com/Kong/volcano-cli/internal/session"
)

// Service performs authenticated Volcano storage workflows.
type Service struct {
	sessions            clisession.Factory
	objectTokenProvider ObjectTokenProvider
}

// ObjectTokenProvider returns the bearer token to use for storage object routes.
// Bucket, policy, and stats workflows continue to use the current user session.
type ObjectTokenProvider func(context.Context) (string, error)

// Option configures storage workflows.
type Option func(*Service)

// WithObjectTokenProvider configures the bearer token source for storage object routes.
func WithObjectTokenProvider(provider ObjectTokenProvider) Option {
	return func(s *Service) {
		s.objectTokenProvider = provider
	}
}

// NewService returns a storage service.
func NewService(deps cliruntime.Deps, opts ...Option) Service {
	service := Service{
		sessions: clisession.NewFactory(deps),
	}
	for _, opt := range opts {
		opt(&service)
	}
	return service
}

// ListBuckets returns every bucket in the current project.
func (s Service) ListBuckets(ctx context.Context) ([]apiclient.StorageBucket, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	buckets, err := authenticated.API.ListStorageBuckets(ctx, authenticated.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("failed to list storage buckets: %w", err)
	}
	return buckets, nil
}

// GetBucket returns one bucket by name.
func (s Service) GetBucket(ctx context.Context, name string) (*apiclient.StorageBucket, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	bucket, err := authenticated.API.GetStorageBucket(ctx, authenticated.ProjectID, name)
	if err != nil {
		return nil, fmt.Errorf("failed to get storage bucket %q: %w", name, err)
	}
	if bucket == nil {
		return nil, fmt.Errorf("failed to get storage bucket %q: server returned empty response", name)
	}
	return bucket, nil
}

// CreateBucket creates one bucket in the current project.
func (s Service) CreateBucket(ctx context.Context, input api.StorageBucketCreateInput) (*apiclient.StorageBucket, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	bucket, err := authenticated.API.CreateStorageBucket(ctx, authenticated.ProjectID, input)
	if err != nil {
		return nil, fmt.Errorf("failed to create storage bucket %q: %w", input.Name, err)
	}
	return bucket, nil
}

// UpdateBucket updates one bucket's configuration.
func (s Service) UpdateBucket(ctx context.Context, name string, input api.StorageBucketUpdateInput) (*apiclient.StorageBucket, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	bucket, err := authenticated.API.UpdateStorageBucket(ctx, authenticated.ProjectID, name, input)
	if err != nil {
		return nil, fmt.Errorf("failed to update storage bucket %q: %w", name, err)
	}
	return bucket, nil
}

// DeleteBucket removes one bucket from the current project.
func (s Service) DeleteBucket(ctx context.Context, name string) error {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return err
	}

	if err := authenticated.API.DeleteStorageBucket(ctx, authenticated.ProjectID, name); err != nil {
		return fmt.Errorf("failed to delete storage bucket %q: %w", name, err)
	}
	return nil
}

// ListPolicies returns every policy attached to a bucket.
func (s Service) ListPolicies(ctx context.Context, bucketName string) ([]apiclient.StoragePolicy, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	policies, err := authenticated.API.ListStoragePolicies(ctx, authenticated.ProjectID, bucketName)
	if err != nil {
		return nil, fmt.Errorf("failed to list storage policies for bucket %q: %w", bucketName, err)
	}
	return policies, nil
}

// GetPolicy returns one policy attached to a bucket by name or UUID. The Volcano
// API does not expose a single-policy fetch endpoint, so this client-side filter
// walks the bucket's policy list.
func (s Service) GetPolicy(ctx context.Context, bucketName, identifier string) (*apiclient.StoragePolicy, error) {
	target := strings.TrimSpace(identifier)
	if target == "" {
		return nil, errors.New("policy identifier cannot be empty")
	}

	policies, err := s.ListPolicies(ctx, bucketName)
	if err != nil {
		return nil, err
	}

	for i := range policies {
		if policies[i].Name == target || policies[i].Id.String() == target {
			return &policies[i], nil
		}
	}
	return nil, fmt.Errorf("policy %q not found on bucket %q", identifier, bucketName)
}

// CreatePolicy attaches a new policy to a bucket.
func (s Service) CreatePolicy(ctx context.Context, bucketName string, input api.StoragePolicyCreateInput) (*apiclient.StoragePolicy, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	policy, err := authenticated.API.CreateStoragePolicy(ctx, authenticated.ProjectID, bucketName, input)
	if err != nil {
		return nil, fmt.Errorf("failed to create storage policy %q on bucket %q: %w", input.Name, bucketName, err)
	}
	return policy, nil
}

// DeletePolicy removes one policy from a bucket by name or UUID.
func (s Service) DeletePolicy(ctx context.Context, bucketName, identifier string) (*apiclient.StoragePolicy, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	policy, err := s.GetPolicy(ctx, bucketName, identifier)
	if err != nil {
		return nil, err
	}

	if err := authenticated.API.DeleteStoragePolicy(ctx, authenticated.ProjectID, bucketName, policy.Id); err != nil {
		return nil, fmt.Errorf("failed to delete storage policy %q on bucket %q: %w", identifier, bucketName, err)
	}
	return policy, nil
}

// ListObjects returns one cursor page of objects in a bucket.
func (s Service) ListObjects(ctx context.Context, bucketName string, opts api.StorageObjectListOptions) (*apiclient.StorageListResponse, error) {
	storageAPI, err := s.objectAPI(ctx)
	if err != nil {
		return nil, err
	}

	page, err := storageAPI.ListStorageObjects(ctx, bucketName, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to list objects in bucket %q: %w", bucketName, err)
	}
	return page, nil
}

// UploadObject uploads one object to a bucket via a streaming multipart
// request, reading the body lazily from content.
func (s Service) UploadObject(ctx context.Context, bucketName, path, contentType string, content io.Reader) (*apiclient.StorageObject, error) {
	storageAPI, err := s.objectAPI(ctx)
	if err != nil {
		return nil, err
	}

	object, err := storageAPI.UploadStorageObject(ctx, bucketName, path, contentType, content)
	if err != nil {
		return nil, fmt.Errorf("failed to upload object %q to bucket %q: %w", path, bucketName, err)
	}
	return object, nil
}

// DownloadObject streams one object body into the writer and returns its size.
func (s Service) DownloadObject(ctx context.Context, bucketName, path string, w io.Writer) (int64, error) {
	storageAPI, err := s.objectAPI(ctx)
	if err != nil {
		return 0, err
	}

	resp, err := storageAPI.DownloadStorageObject(ctx, bucketName, path)
	if err != nil {
		return 0, fmt.Errorf("failed to download object %q from bucket %q: %w", path, bucketName, err)
	}
	defer func() { _ = resp.Body.Close() }()

	written, err := io.Copy(w, resp.Body)
	if err != nil {
		return written, fmt.Errorf("failed to write object %q from bucket %q: %w", path, bucketName, err)
	}
	return written, nil
}

// DeleteObject removes one object from a bucket.
func (s Service) DeleteObject(ctx context.Context, bucketName, path string) error {
	storageAPI, err := s.objectAPI(ctx)
	if err != nil {
		return err
	}

	if err := storageAPI.DeleteStorageObject(ctx, bucketName, path); err != nil {
		return fmt.Errorf("failed to delete object %q from bucket %q: %w", path, bucketName, err)
	}
	return nil
}

// CopyObject copies one object within a bucket.
func (s Service) CopyObject(ctx context.Context, bucketName, from, to string) (*apiclient.StorageObject, error) {
	storageAPI, err := s.objectAPI(ctx)
	if err != nil {
		return nil, err
	}

	object, err := storageAPI.CopyStorageObject(ctx, bucketName, from, to)
	if err != nil {
		return nil, fmt.Errorf("failed to copy object %q to %q in bucket %q: %w", from, to, bucketName, err)
	}
	return object, nil
}

// MoveObject renames one object within a bucket.
func (s Service) MoveObject(ctx context.Context, bucketName, from, to string) (*apiclient.StorageObject, error) {
	storageAPI, err := s.objectAPI(ctx)
	if err != nil {
		return nil, err
	}

	object, err := storageAPI.MoveStorageObject(ctx, bucketName, from, to)
	if err != nil {
		return nil, fmt.Errorf("failed to move object %q to %q in bucket %q: %w", from, to, bucketName, err)
	}
	return object, nil
}

// SetObjectVisibility updates the public/private flag on one object.
func (s Service) SetObjectVisibility(ctx context.Context, bucketName, path string, isPublic bool) (*apiclient.StorageObject, error) {
	storageAPI, err := s.objectAPI(ctx)
	if err != nil {
		return nil, err
	}

	object, err := storageAPI.SetStorageObjectVisibility(ctx, bucketName, path, isPublic)
	if err != nil {
		return nil, fmt.Errorf("failed to update visibility for object %q in bucket %q: %w", path, bucketName, err)
	}
	if object == nil {
		return nil, fmt.Errorf("failed to update visibility for object %q in bucket %q: server returned empty response", path, bucketName)
	}
	return object, nil
}

// GetStats returns aggregate storage statistics for the current project.
func (s Service) GetStats(ctx context.Context) (*apiclient.StorageStats, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	stats, err := authenticated.API.GetStorageStats(ctx, authenticated.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch storage stats: %w", err)
	}
	return stats, nil
}

func (s Service) objectAPI(ctx context.Context) (*api.Client, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	if s.objectTokenProvider == nil {
		return authenticated.API, nil
	}
	storageToken, err := s.objectTokenProvider(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load storage object token: %w", err)
	}
	storageToken = strings.TrimSpace(storageToken)
	if storageToken == "" || storageToken == strings.TrimSpace(authenticated.Config.Token()) {
		return authenticated.API, nil
	}

	storageAPI, err := authenticated.APIWithToken(storageToken)
	if err != nil {
		return nil, fmt.Errorf("failed to create storage api client: %w", err)
	}
	return storageAPI, nil
}
