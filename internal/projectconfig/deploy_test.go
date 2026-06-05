package projectconfig

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
	apicommon "github.com/Kong/volcano-cli/internal/apiclient/common"
)

// fakeStorage records calls and lets tests script its responses.
type fakeStorage struct {
	buckets        map[string]*apiclient.StorageBucket
	policies       map[string][]apiclient.StoragePolicy
	createBuckets  []api.StorageBucketCreateInput
	updateBuckets  []bucketUpdateCall
	createPolicies []policyCreateCall
	deletePolicies []policyDeleteCall

	getBucketErr     error
	createErr        error
	updateErr        error
	listPoliciesErr  error
	createPolicyErr  error
	createPolicyErrs []error
	deletePolicyErr  error
}

type bucketUpdateCall struct {
	Name  string
	Input api.StorageBucketUpdateInput
}

type policyCreateCall struct {
	Bucket string
	Input  api.StoragePolicyCreateInput
}

type policyDeleteCall struct {
	Bucket     string
	Identifier string
}

func newFakeStorage() *fakeStorage {
	return &fakeStorage{
		buckets:  make(map[string]*apiclient.StorageBucket),
		policies: make(map[string][]apiclient.StoragePolicy),
	}
}

func (f *fakeStorage) GetBucket(_ context.Context, name string) (*apiclient.StorageBucket, error) {
	if f.getBucketErr != nil {
		return nil, f.getBucketErr
	}
	bucket, ok := f.buckets[name]
	if !ok {
		return nil, &api.Error{StatusCode: http.StatusNotFound, Message: "not found"}
	}
	return bucket, nil
}

func (f *fakeStorage) CreateBucket(_ context.Context, input api.StorageBucketCreateInput) (*apiclient.StorageBucket, error) {
	f.createBuckets = append(f.createBuckets, input)
	if f.createErr != nil {
		return nil, f.createErr
	}
	bucket := &apiclient.StorageBucket{
		Id:            uuid.New(),
		Name:          input.Name,
		FileSizeLimit: input.FileSizeLimit,
	}
	if len(input.AllowedMimeTypes) > 0 {
		mimes := append([]string(nil), input.AllowedMimeTypes...)
		bucket.AllowedMimeTypes = &mimes
	}
	f.buckets[input.Name] = bucket
	return bucket, nil
}

func (f *fakeStorage) UpdateBucket(_ context.Context, name string, input api.StorageBucketUpdateInput) (*apiclient.StorageBucket, error) {
	f.updateBuckets = append(f.updateBuckets, bucketUpdateCall{Name: name, Input: input})
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	bucket, ok := f.buckets[name]
	if !ok {
		return nil, fmt.Errorf("bucket %q not found", name)
	}
	if input.FileSizeLimit != nil {
		size := *input.FileSizeLimit
		bucket.FileSizeLimit = &size
	}
	if input.AllowedMimeTypes != nil {
		mimes := append([]string(nil), (*input.AllowedMimeTypes)...)
		bucket.AllowedMimeTypes = &mimes
	}
	return bucket, nil
}

func (f *fakeStorage) ListPolicies(_ context.Context, bucketName string) ([]apiclient.StoragePolicy, error) {
	if f.listPoliciesErr != nil {
		return nil, f.listPoliciesErr
	}
	return append([]apiclient.StoragePolicy(nil), f.policies[bucketName]...), nil
}

func (f *fakeStorage) CreatePolicy(_ context.Context, bucketName string, input api.StoragePolicyCreateInput) (*apiclient.StoragePolicy, error) {
	f.createPolicies = append(f.createPolicies, policyCreateCall{Bucket: bucketName, Input: input})
	if len(f.createPolicyErrs) > 0 {
		err := f.createPolicyErrs[0]
		f.createPolicyErrs = f.createPolicyErrs[1:]
		if err != nil {
			return nil, err
		}
	} else if f.createPolicyErr != nil {
		return nil, f.createPolicyErr
	}
	policy := apiclient.StoragePolicy{
		Id:         uuid.New(),
		BucketId:   uuid.New(),
		Name:       input.Name,
		Operation:  apicommon.StoragePolicyOperation(input.Operation),
		Definition: input.Definition,
	}
	f.policies[bucketName] = append(f.policies[bucketName], policy)
	return &policy, nil
}

func (f *fakeStorage) DeletePolicy(_ context.Context, bucketName, identifier string) (*apiclient.StoragePolicy, error) {
	f.deletePolicies = append(f.deletePolicies, policyDeleteCall{Bucket: bucketName, Identifier: identifier})
	if f.deletePolicyErr != nil {
		return nil, f.deletePolicyErr
	}
	existing := f.policies[bucketName]
	for i := range existing {
		if existing[i].Id.String() == identifier || existing[i].Name == identifier {
			deleted := existing[i]
			f.policies[bucketName] = append(existing[:i], existing[i+1:]...)
			return &deleted, nil
		}
	}
	return nil, fmt.Errorf("policy %q not found", identifier)
}

type fakeFunctions struct {
	functions      []apiclient.Function
	visibilitySets []visibilityCall
	listErr        error
	updateErr      error
}

type visibilityCall struct {
	Identifier string
	IsPublic   bool
}

func (f *fakeFunctions) ListPage(_ context.Context, page, _ int) (*apiclient.PaginatedFunctions, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	if page != api.DefaultPage {
		return &apiclient.PaginatedFunctions{Page: page, HasMore: false}, nil
	}
	return &apiclient.PaginatedFunctions{
		Page:    page,
		Limit:   api.DefaultLimit,
		Total:   len(f.functions),
		Data:    append([]apiclient.Function(nil), f.functions...),
		HasMore: false,
	}, nil
}

func (f *fakeFunctions) UpdateVisibility(_ context.Context, identifier string, isPublic bool) (*apiclient.Function, error) {
	f.visibilitySets = append(f.visibilitySets, visibilityCall{Identifier: identifier, IsPublic: isPublic})
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	for i := range f.functions {
		if f.functions[i].Name == identifier || f.functions[i].Id.String() == identifier {
			f.functions[i].IsPublic = isPublic
			fn := f.functions[i]
			return &fn, nil
		}
	}
	return nil, fmt.Errorf("function %q not found", identifier)
}

func TestDeployCreatesMissingBucketAndPolicies(t *testing.T) {
	storage := newFakeStorage()
	functions := &fakeFunctions{}
	svc := NewServiceWithReconcilers(storage, functions)

	limit := int64(2048)
	manifest := &Manifest{
		Version: 1,
		Buckets: []BucketManifest{{
			Name:             "uploads",
			FileSizeLimit:    &limit,
			AllowedMimeTypes: &[]string{"image/png", "image/jpeg"},
			Policies: []PolicyManifest{
				{Name: "owner", Operation: "SELECT", Definition: "auth.uid() = owner_id"},
			},
		}},
	}

	summary, err := svc.Deploy(context.Background(), manifest)
	require.NoError(t, err)
	assert.Equal(t, 1, summary.BucketsCreated)
	assert.Equal(t, 0, summary.BucketsUpdated)
	assert.Equal(t, 0, summary.BucketsUnchanged)
	assert.Equal(t, 1, summary.PoliciesCreated)
	assert.Equal(t, 0, summary.PoliciesUpdated)
	assert.Equal(t, 0, summary.PoliciesUnchanged)
	assert.Equal(t, 0, summary.PoliciesDeleted)

	require.Len(t, storage.createBuckets, 1)
	assert.Equal(t, "uploads", storage.createBuckets[0].Name)
	require.NotNil(t, storage.createBuckets[0].FileSizeLimit)
	assert.EqualValues(t, 2048, *storage.createBuckets[0].FileSizeLimit)
	assert.Equal(t, []string{"image/png", "image/jpeg"}, storage.createBuckets[0].AllowedMimeTypes)

	require.Len(t, storage.createPolicies, 1)
	assert.Equal(t, "uploads", storage.createPolicies[0].Bucket)
	assert.Equal(t, "owner", storage.createPolicies[0].Input.Name)
	assert.Equal(t, apicommon.CreateStoragePolicyRequestOperationSELECT, storage.createPolicies[0].Input.Operation)
}

func TestDeployUpdatesBucketWhenMimeTypesDiffer(t *testing.T) {
	storage := newFakeStorage()
	existingLimit := int64(1024)
	storage.buckets["uploads"] = &apiclient.StorageBucket{
		Id:               uuid.New(),
		Name:             "uploads",
		FileSizeLimit:    &existingLimit,
		AllowedMimeTypes: &[]string{"image/png"},
	}

	svc := NewServiceWithReconcilers(storage, &fakeFunctions{})
	newLimit := int64(1024)
	manifest := &Manifest{
		Version: 1,
		Buckets: []BucketManifest{{
			Name:             "uploads",
			FileSizeLimit:    &newLimit,
			AllowedMimeTypes: &[]string{"image/png", "image/jpeg"},
		}},
	}

	summary, err := svc.Deploy(context.Background(), manifest)
	require.NoError(t, err)
	assert.Equal(t, 0, summary.BucketsCreated)
	assert.Equal(t, 1, summary.BucketsUpdated)
	require.Len(t, storage.updateBuckets, 1)
	require.NotNil(t, storage.updateBuckets[0].Input.AllowedMimeTypes)
	assert.Equal(t, []string{"image/png", "image/jpeg"}, *storage.updateBuckets[0].Input.AllowedMimeTypes)
	require.NotNil(t, storage.updateBuckets[0].Input.FileSizeLimit,
		"update payload must carry FileSizeLimit so a partial update can't clear the server value")
	assert.Equal(t, newLimit, *storage.updateBuckets[0].Input.FileSizeLimit)
}

func TestDeployLeavesBucketUnchangedWhenFieldsMatch(t *testing.T) {
	storage := newFakeStorage()
	limit := int64(1024)
	storage.buckets["uploads"] = &apiclient.StorageBucket{
		Id:               uuid.New(),
		Name:             "uploads",
		FileSizeLimit:    &limit,
		AllowedMimeTypes: &[]string{"image/png"},
	}

	svc := NewServiceWithReconcilers(storage, &fakeFunctions{})
	manifest := &Manifest{
		Version: 1,
		Buckets: []BucketManifest{{
			Name:             "uploads",
			FileSizeLimit:    &limit,
			AllowedMimeTypes: &[]string{"image/png"},
		}},
	}

	summary, err := svc.Deploy(context.Background(), manifest)
	require.NoError(t, err)
	assert.Equal(t, 1, summary.BucketsUnchanged)
	assert.Empty(t, storage.updateBuckets)
}

func TestDeployRecreatesPolicyWhenDefinitionChanges(t *testing.T) {
	storage := newFakeStorage()
	bucketID := uuid.New()
	policyID := uuid.New()
	storage.buckets["uploads"] = &apiclient.StorageBucket{Id: bucketID, Name: "uploads"}
	storage.policies["uploads"] = []apiclient.StoragePolicy{{
		Id:         policyID,
		BucketId:   bucketID,
		Name:       "owner",
		Operation:  apicommon.StoragePolicyOperationSELECT,
		Definition: "true",
	}}

	svc := NewServiceWithReconcilers(storage, &fakeFunctions{})
	manifest := &Manifest{
		Version: 1,
		Buckets: []BucketManifest{{
			Name: "uploads",
			Policies: []PolicyManifest{
				{Name: "owner", Operation: "SELECT", Definition: "auth.uid() = owner_id"},
			},
		}},
	}

	summary, err := svc.Deploy(context.Background(), manifest)
	require.NoError(t, err)
	assert.Equal(t, 1, summary.PoliciesUpdated)
	assert.Equal(t, 0, summary.PoliciesUnchanged)

	require.Len(t, storage.deletePolicies, 1)
	assert.Equal(t, policyID.String(), storage.deletePolicies[0].Identifier)
	require.Len(t, storage.createPolicies, 1)
	assert.Equal(t, "auth.uid() = owner_id", storage.createPolicies[0].Input.Definition)
}

func TestDeployDeletesPolicyMissingFromManifest(t *testing.T) {
	storage := newFakeStorage()
	bucketID := uuid.New()
	storage.buckets["uploads"] = &apiclient.StorageBucket{Id: bucketID, Name: "uploads"}
	staleID := uuid.New()
	storage.policies["uploads"] = []apiclient.StoragePolicy{{
		Id:         staleID,
		BucketId:   bucketID,
		Name:       "old-policy",
		Operation:  apicommon.StoragePolicyOperationSELECT,
		Definition: "false",
	}}

	svc := NewServiceWithReconcilers(storage, &fakeFunctions{})
	manifest := &Manifest{
		Version: 1,
		Buckets: []BucketManifest{{Name: "uploads"}},
	}

	summary, err := svc.Deploy(context.Background(), manifest)
	require.NoError(t, err)
	assert.Equal(t, 1, summary.PoliciesDeleted)
	require.Len(t, storage.deletePolicies, 1)
	assert.Equal(t, staleID.String(), storage.deletePolicies[0].Identifier)
}

func TestDeployUpdatesFunctionVisibility(t *testing.T) {
	helloID := uuid.New()
	functions := &fakeFunctions{
		functions: []apiclient.Function{
			{Id: helloID, Name: "hello", IsPublic: false},
			{Id: uuid.New(), Name: "world", IsPublic: true},
		},
	}
	svc := NewServiceWithReconcilers(newFakeStorage(), functions)

	pub := true
	stay := true
	manifest := &Manifest{
		Version: 1,
		Functions: []FunctionManifest{
			{Name: "hello", Public: &pub},
			{Name: "world", Public: &stay},
		},
	}

	summary, err := svc.Deploy(context.Background(), manifest)
	require.NoError(t, err)
	assert.Equal(t, 1, summary.FunctionsUpdated)
	assert.Equal(t, 1, summary.FunctionsUnchanged)
	require.Len(t, functions.visibilitySets, 1)
	assert.Equal(t, helloID.String(), functions.visibilitySets[0].Identifier,
		"visibility update must target the UUID we already fetched, not re-resolve by name")
	assert.True(t, functions.visibilitySets[0].IsPublic)
}

func TestDeployFunctionMissingReturnsAvailableList(t *testing.T) {
	functions := &fakeFunctions{
		functions: []apiclient.Function{
			{Id: uuid.New(), Name: "hello"},
			{Id: uuid.New(), Name: "world"},
		},
	}
	svc := NewServiceWithReconcilers(newFakeStorage(), functions)
	pub := true
	manifest := &Manifest{
		Version:   1,
		Functions: []FunctionManifest{{Name: "missing", Public: &pub}},
	}

	_, err := svc.Deploy(context.Background(), manifest)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `function "missing" not found`)
	assert.Contains(t, err.Error(), "Available functions: hello, world")
}

func TestDeployFunctionMissingNoFunctions(t *testing.T) {
	svc := NewServiceWithReconcilers(newFakeStorage(), &fakeFunctions{})
	pub := true
	manifest := &Manifest{
		Version:   1,
		Functions: []FunctionManifest{{Name: "missing", Public: &pub}},
	}

	_, err := svc.Deploy(context.Background(), manifest)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "project has no deployed functions")
}

func TestDeployBucketFetchErrorPropagates(t *testing.T) {
	storage := newFakeStorage()
	storage.getBucketErr = errors.New("boom")
	svc := NewServiceWithReconcilers(storage, &fakeFunctions{})

	manifest := &Manifest{
		Version: 1,
		Buckets: []BucketManifest{{Name: "uploads"}},
	}
	_, err := svc.Deploy(context.Background(), manifest)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `bucket "uploads": failed to fetch`)
}

func TestDeployNilManifest(t *testing.T) {
	svc := NewServiceWithReconcilers(newFakeStorage(), &fakeFunctions{})
	_, err := svc.Deploy(context.Background(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "manifest is required")
}

func TestDeployRollsBackPolicyWhenRecreateFails(t *testing.T) {
	storage := newFakeStorage()
	bucketID := uuid.New()
	policyID := uuid.New()
	storage.buckets["uploads"] = &apiclient.StorageBucket{Id: bucketID, Name: "uploads"}
	storage.policies["uploads"] = []apiclient.StoragePolicy{{
		Id:         policyID,
		BucketId:   bucketID,
		Name:       "owner",
		Operation:  apicommon.StoragePolicyOperationSELECT,
		Definition: "true",
	}}
	// Fail the new-definition create; let the rollback create succeed.
	storage.createPolicyErrs = []error{errors.New("boom"), nil}

	svc := NewServiceWithReconcilers(storage, &fakeFunctions{})
	manifest := &Manifest{
		Version: 1,
		Buckets: []BucketManifest{{
			Name: "uploads",
			Policies: []PolicyManifest{
				{Name: "owner", Operation: "SELECT", Definition: "auth.uid() = owner_id"},
			},
		}},
	}

	_, err := svc.Deploy(context.Background(), manifest)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `failed to recreate policy "owner"`)
	assert.Contains(t, err.Error(), "rolled back to previous definition")

	require.Len(t, storage.createPolicies, 2)
	assert.Equal(t, "auth.uid() = owner_id", storage.createPolicies[0].Input.Definition)
	assert.Equal(t, "true", storage.createPolicies[1].Input.Definition,
		"rollback must restore the previous policy definition")
	assert.Equal(t, "owner", storage.createPolicies[1].Input.Name)
	assert.Equal(t, apicommon.CreateStoragePolicyRequestOperationSELECT, storage.createPolicies[1].Input.Operation)
}

func TestDeployPolicyRollbackFailureSurfacesBothErrors(t *testing.T) {
	storage := newFakeStorage()
	bucketID := uuid.New()
	policyID := uuid.New()
	storage.buckets["uploads"] = &apiclient.StorageBucket{Id: bucketID, Name: "uploads"}
	storage.policies["uploads"] = []apiclient.StoragePolicy{{
		Id:         policyID,
		BucketId:   bucketID,
		Name:       "owner",
		Operation:  apicommon.StoragePolicyOperationSELECT,
		Definition: "true",
	}}
	storage.createPolicyErrs = []error{errors.New("create failed"), errors.New("rollback failed")}

	svc := NewServiceWithReconcilers(storage, &fakeFunctions{})
	manifest := &Manifest{
		Version: 1,
		Buckets: []BucketManifest{{
			Name: "uploads",
			Policies: []PolicyManifest{
				{Name: "owner", Operation: "SELECT", Definition: "auth.uid() = owner_id"},
			},
		}},
	}

	_, err := svc.Deploy(context.Background(), manifest)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create failed")
	assert.Contains(t, err.Error(), "rollback to previous definition also failed")
	assert.Contains(t, err.Error(), "rollback failed")
}

// pagedFunctions returns one apiclient.Function per page so we can exercise
// listAllFunctions's pagination loop beyond page 1.
type pagedFunctions struct {
	pages          [][]apiclient.Function
	visibilitySets []visibilityCall
}

func (p *pagedFunctions) ListPage(_ context.Context, page, _ int) (*apiclient.PaginatedFunctions, error) {
	idx := page - api.DefaultPage
	if idx < 0 || idx >= len(p.pages) {
		return &apiclient.PaginatedFunctions{Page: page, HasMore: false}, nil
	}
	return &apiclient.PaginatedFunctions{
		Page:    page,
		Data:    append([]apiclient.Function(nil), p.pages[idx]...),
		HasMore: idx < len(p.pages)-1,
	}, nil
}

func (p *pagedFunctions) UpdateVisibility(_ context.Context, identifier string, isPublic bool) (*apiclient.Function, error) {
	p.visibilitySets = append(p.visibilitySets, visibilityCall{Identifier: identifier, IsPublic: isPublic})
	for _, page := range p.pages {
		for i := range page {
			if page[i].Id.String() == identifier || page[i].Name == identifier {
				page[i].IsPublic = isPublic
				fn := page[i]
				return &fn, nil
			}
		}
	}
	return nil, fmt.Errorf("function %q not found", identifier)
}

func TestDeployListsFunctionsAcrossMultiplePages(t *testing.T) {
	firstID := uuid.New()
	secondID := uuid.New()
	functions := &pagedFunctions{
		pages: [][]apiclient.Function{
			{{Id: firstID, Name: "first", IsPublic: false}},
			{{Id: secondID, Name: "second", IsPublic: false}},
		},
	}
	svc := NewServiceWithReconcilers(newFakeStorage(), functions)

	pub := true
	manifest := &Manifest{
		Version: 1,
		Functions: []FunctionManifest{
			{Name: "first", Public: &pub},
			{Name: "second", Public: &pub},
		},
	}

	summary, err := svc.Deploy(context.Background(), manifest)
	require.NoError(t, err)
	assert.Equal(t, 2, summary.FunctionsUpdated,
		"both functions must be reachable, including the one on page 2")
	require.Len(t, functions.visibilitySets, 2)
	assert.Equal(t, firstID.String(), functions.visibilitySets[0].Identifier)
	assert.Equal(t, secondID.String(), functions.visibilitySets[1].Identifier)
}

func TestDeployLeavesBucketUnchangedWhenMimeTypesDifferInOrder(t *testing.T) {
	storage := newFakeStorage()
	limit := int64(1024)
	storage.buckets["uploads"] = &apiclient.StorageBucket{
		Id:               uuid.New(),
		Name:             "uploads",
		FileSizeLimit:    &limit,
		AllowedMimeTypes: &[]string{"image/jpeg", "image/png"},
	}

	svc := NewServiceWithReconcilers(storage, &fakeFunctions{})
	manifest := &Manifest{
		Version: 1,
		Buckets: []BucketManifest{{
			Name:             "uploads",
			FileSizeLimit:    &limit,
			AllowedMimeTypes: &[]string{"image/png", "image/jpeg"},
		}},
	}

	summary, err := svc.Deploy(context.Background(), manifest)
	require.NoError(t, err)
	assert.Equal(t, 1, summary.BucketsUnchanged)
	assert.Empty(t, storage.updateBuckets, "MIME types differing only in order must not trigger an update")
}
