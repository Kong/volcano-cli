package projectconfig

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
	apicommon "github.com/Kong/volcano-cli/internal/apiclient/common"
	clifunction "github.com/Kong/volcano-cli/internal/function"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	clistorage "github.com/Kong/volcano-cli/internal/storage"
)

// Summary describes what changed in one Deploy run. All counters are
// non-decreasing across reconciliation steps so callers can render a single
// "buckets: X created, Y updated, Z unchanged" line per resource type.
type Summary struct {
	BucketsCreated     int
	BucketsUpdated     int
	BucketsUnchanged   int
	PoliciesCreated    int
	PoliciesUpdated    int
	PoliciesDeleted    int
	PoliciesUnchanged  int
	FunctionsUpdated   int
	FunctionsUnchanged int
}

// StorageReconciler is the subset of internal/storage.Service that Deploy
// drives. It is defined here so tests can substitute a fake implementation
// without standing up an authenticated session.
type StorageReconciler interface {
	GetBucket(ctx context.Context, name string) (*apiclient.StorageBucket, error)
	CreateBucket(ctx context.Context, input api.StorageBucketCreateInput) (*apiclient.StorageBucket, error)
	UpdateBucket(ctx context.Context, name string, input api.StorageBucketUpdateInput) (*apiclient.StorageBucket, error)
	ListPolicies(ctx context.Context, bucketName string) ([]apiclient.StoragePolicy, error)
	CreatePolicy(ctx context.Context, bucketName string, input api.StoragePolicyCreateInput) (*apiclient.StoragePolicy, error)
	DeletePolicy(ctx context.Context, bucketName, identifier string) (*apiclient.StoragePolicy, error)
}

// FunctionReconciler is the subset of internal/function.Service used to update
// function visibility from a manifest.
type FunctionReconciler interface {
	ListPage(ctx context.Context, page, limit int) (*apiclient.PaginatedFunctions, error)
	UpdateVisibility(ctx context.Context, identifier string, isPublic bool) (*apiclient.Function, error)
}

// Service deploys declarative project configuration to the Volcano API.
type Service struct {
	storage   StorageReconciler
	functions FunctionReconciler
}

// NewService wires the projectconfig Service against the storage and function
// services.
func NewService(deps cliruntime.Deps) Service {
	return Service{
		storage:   clistorage.NewService(deps),
		functions: clifunction.NewService(deps),
	}
}

// NewServiceWithReconcilers returns a Service that uses the supplied
// reconcilers. Intended for tests.
func NewServiceWithReconcilers(storage StorageReconciler, functions FunctionReconciler) Service {
	return Service{storage: storage, functions: functions}
}

// Deploy reconciles the project state to match the manifest. Buckets and their
// policies are processed in manifest order; function visibility updates run
// after storage so an operator can see partial progress in the summary even
// when the storage phase fails.
func (s Service) Deploy(ctx context.Context, manifest *Manifest) (*Summary, error) {
	if manifest == nil {
		return nil, errors.New("manifest is required")
	}

	summary := &Summary{}
	for _, bucket := range manifest.Buckets {
		if err := s.reconcileBucket(ctx, bucket, summary); err != nil {
			return summary, err
		}
		if err := s.reconcilePolicies(ctx, bucket, summary); err != nil {
			return summary, err
		}
	}

	if err := s.reconcileFunctions(ctx, manifest.Functions, summary); err != nil {
		return summary, err
	}
	return summary, nil
}

func (s Service) reconcileBucket(ctx context.Context, bucket BucketManifest, summary *Summary) error {
	existing, err := s.storage.GetBucket(ctx, bucket.Name)
	if err != nil {
		if api.Status(err) == http.StatusNotFound {
			input := api.StorageBucketCreateInput{
				Name:          bucket.Name,
				FileSizeLimit: bucket.FileSizeLimit,
			}
			if bucket.AllowedMimeTypes != nil {
				input.AllowedMimeTypes = append([]string(nil), (*bucket.AllowedMimeTypes)...)
			}
			if _, createErr := s.storage.CreateBucket(ctx, input); createErr != nil {
				return fmt.Errorf("bucket %q: failed to create: %w", bucket.Name, createErr)
			}
			summary.BucketsCreated++
			return nil
		}
		return fmt.Errorf("bucket %q: failed to fetch: %w", bucket.Name, err)
	}

	if !bucketNeedsUpdate(existing, bucket) {
		summary.BucketsUnchanged++
		return nil
	}

	update := api.StorageBucketUpdateInput{}
	if bucket.FileSizeLimit != nil {
		size := *bucket.FileSizeLimit
		update.FileSizeLimit = &size
	}
	if bucket.AllowedMimeTypes != nil {
		mimes := append([]string(nil), (*bucket.AllowedMimeTypes)...)
		update.AllowedMimeTypes = &mimes
	}
	if _, err := s.storage.UpdateBucket(ctx, bucket.Name, update); err != nil {
		return fmt.Errorf("bucket %q: failed to update: %w", bucket.Name, err)
	}
	summary.BucketsUpdated++
	return nil
}

func bucketNeedsUpdate(existing *apiclient.StorageBucket, desired BucketManifest) bool {
	if existing == nil {
		return true
	}
	if desired.FileSizeLimit != nil {
		if existing.FileSizeLimit == nil || *existing.FileSizeLimit != *desired.FileSizeLimit {
			return true
		}
	}
	if desired.AllowedMimeTypes != nil {
		var current []string
		if existing.AllowedMimeTypes != nil {
			current = *existing.AllowedMimeTypes
		}
		if !stringSlicesEqual(current, *desired.AllowedMimeTypes) {
			return true
		}
	}
	return false
}

func (s Service) reconcilePolicies(ctx context.Context, bucket BucketManifest, summary *Summary) error {
	existing, err := s.storage.ListPolicies(ctx, bucket.Name)
	if err != nil {
		return fmt.Errorf("bucket %q: failed to list policies: %w", bucket.Name, err)
	}

	desired := make(map[string]PolicyManifest, len(bucket.Policies))
	for _, policy := range bucket.Policies {
		desired[policy.Name] = policy
	}

	handled := make(map[string]bool, len(existing))
	for i := range existing {
		current := existing[i]
		target, want := desired[current.Name]
		if !want {
			if _, err := s.storage.DeletePolicy(ctx, bucket.Name, current.Id.String()); err != nil {
				return fmt.Errorf("bucket %q: failed to delete policy %q: %w", bucket.Name, current.Name, err)
			}
			summary.PoliciesDeleted++
			continue
		}
		handled[current.Name] = true

		if string(current.Operation) == target.Operation && strings.TrimSpace(current.Definition) == strings.TrimSpace(target.Definition) {
			summary.PoliciesUnchanged++
			continue
		}

		if _, err := s.storage.DeletePolicy(ctx, bucket.Name, current.Id.String()); err != nil {
			return fmt.Errorf("bucket %q: failed to replace policy %q: %w", bucket.Name, current.Name, err)
		}
		if _, err := s.storage.CreatePolicy(ctx, bucket.Name, policyCreateInput(target)); err != nil {
			// Best-effort rollback: the API has no atomic policy update, so on
			// create failure we recreate the previous definition to avoid
			// leaving the bucket without a policy.
			rollback := api.StoragePolicyCreateInput{
				Name:       current.Name,
				Definition: current.Definition,
				Operation:  apicommon.CreateStoragePolicyRequestOperation(current.Operation),
			}
			if _, rollbackErr := s.storage.CreatePolicy(ctx, bucket.Name, rollback); rollbackErr != nil {
				return fmt.Errorf("bucket %q: failed to recreate policy %q: %w; rollback to previous definition also failed: %w", bucket.Name, target.Name, err, rollbackErr)
			}
			return fmt.Errorf("bucket %q: failed to recreate policy %q (rolled back to previous definition): %w", bucket.Name, target.Name, err)
		}
		summary.PoliciesUpdated++
	}

	for _, target := range bucket.Policies {
		if handled[target.Name] {
			continue
		}
		if _, err := s.storage.CreatePolicy(ctx, bucket.Name, policyCreateInput(target)); err != nil {
			return fmt.Errorf("bucket %q: failed to create policy %q: %w", bucket.Name, target.Name, err)
		}
		summary.PoliciesCreated++
	}
	return nil
}

func policyCreateInput(policy PolicyManifest) api.StoragePolicyCreateInput {
	return api.StoragePolicyCreateInput{
		Name:       policy.Name,
		Definition: policy.Definition,
		Operation:  apicommon.CreateStoragePolicyRequestOperation(policy.Operation),
	}
}

func (s Service) reconcileFunctions(ctx context.Context, functions []FunctionManifest, summary *Summary) error {
	if len(functions) == 0 {
		return nil
	}

	deployed, err := listAllFunctions(ctx, s.functions)
	if err != nil {
		return fmt.Errorf("failed to list functions: %w", err)
	}

	byName := make(map[string]apiclient.Function, len(deployed))
	for _, fn := range deployed {
		byName[fn.Name] = fn
	}

	for _, target := range functions {
		fn, ok := byName[target.Name]
		if !ok {
			available := make([]string, 0, len(deployed))
			for _, existing := range deployed {
				available = append(available, existing.Name)
			}
			sort.Strings(available)
			if len(available) == 0 {
				return fmt.Errorf("function %q not found: project has no deployed functions", target.Name)
			}
			return fmt.Errorf("function %q not found. Available functions: %s", target.Name, strings.Join(available, ", "))
		}

		desiredPublic := *target.Public
		if fn.IsPublic == desiredPublic {
			summary.FunctionsUnchanged++
			continue
		}

		if _, err := s.functions.UpdateVisibility(ctx, fn.Id.String(), desiredPublic); err != nil {
			return fmt.Errorf("function %q: failed to update visibility: %w", target.Name, err)
		}
		summary.FunctionsUpdated++
	}
	return nil
}

// listAllFunctions walks every paginated page of project functions. The
// function visibility step needs the full set so it can report a useful
// "available functions" hint when a manifest entry doesn't match anything.
func listAllFunctions(ctx context.Context, functions FunctionReconciler) ([]apiclient.Function, error) {
	var all []apiclient.Function
	for page := api.DefaultPage; ; page++ {
		resp, err := functions.ListPage(ctx, page, api.DefaultLimit)
		if err != nil {
			return nil, err
		}
		if resp == nil {
			return all, nil
		}
		all = append(all, resp.Data...)
		if !resp.HasMore || len(resp.Data) == 0 {
			return all, nil
		}
	}
}

// stringSlicesEqual reports whether a and b contain the same elements
// regardless of order. The API and the manifest may serialize values like
// allowed MIME types in different orders, so positional comparison would flag
// equivalent content as changed and trigger spurious updates.
func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	aSorted := append([]string(nil), a...)
	bSorted := append([]string(nil), b...)
	sort.Strings(aSorted)
	sort.Strings(bSorted)
	for i := range aSorted {
		if aSorted[i] != bSorted[i] {
			return false
		}
	}
	return true
}
