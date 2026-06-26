package projectconfig

import (
	"context"
	"errors"
	"fmt"
	"maps"
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
	svc := NewServiceWithReconcilers(storage, functions, newFakeSchedulers())

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

	svc := NewServiceWithReconcilers(storage, &fakeFunctions{}, newFakeSchedulers())
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

	svc := NewServiceWithReconcilers(storage, &fakeFunctions{}, newFakeSchedulers())
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

	svc := NewServiceWithReconcilers(storage, &fakeFunctions{}, newFakeSchedulers())
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

	svc := NewServiceWithReconcilers(storage, &fakeFunctions{}, newFakeSchedulers())
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
	svc := NewServiceWithReconcilers(newFakeStorage(), functions, newFakeSchedulers())

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
	svc := NewServiceWithReconcilers(newFakeStorage(), functions, newFakeSchedulers())
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
	svc := NewServiceWithReconcilers(newFakeStorage(), &fakeFunctions{}, newFakeSchedulers())
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
	svc := NewServiceWithReconcilers(storage, &fakeFunctions{}, newFakeSchedulers())

	manifest := &Manifest{
		Version: 1,
		Buckets: []BucketManifest{{Name: "uploads"}},
	}
	_, err := svc.Deploy(context.Background(), manifest)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `bucket "uploads": failed to fetch`)
}

func TestDeployNilManifest(t *testing.T) {
	svc := NewServiceWithReconcilers(newFakeStorage(), &fakeFunctions{}, newFakeSchedulers())
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

	svc := NewServiceWithReconcilers(storage, &fakeFunctions{}, newFakeSchedulers())
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

	svc := NewServiceWithReconcilers(storage, &fakeFunctions{}, newFakeSchedulers())
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
	svc := NewServiceWithReconcilers(newFakeStorage(), functions, newFakeSchedulers())

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

	svc := NewServiceWithReconcilers(storage, &fakeFunctions{}, newFakeSchedulers())
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

type fakeSchedulers struct {
	functions    map[string]*apiclient.Function
	schedulers   map[string][]apiclient.FunctionScheduler
	createdCalls []schedulerCreateCall
	updatedCalls []schedulerUpdateCall
	listErr      error
	createErr    error
	updateErr    error
}

type schedulerCreateCall struct {
	FunctionID uuid.UUID
	Input      api.FunctionSchedulerInput
}

type schedulerUpdateCall struct {
	FunctionID  uuid.UUID
	SchedulerID uuid.UUID
	Input       api.FunctionSchedulerInput
}

func newFakeSchedulers() *fakeSchedulers {
	return &fakeSchedulers{
		functions:  make(map[string]*apiclient.Function),
		schedulers: make(map[string][]apiclient.FunctionScheduler),
	}
}

func (f *fakeSchedulers) ListSchedulers(_ context.Context, identifier string) (*apiclient.Function, *apiclient.FunctionSchedulerListResponse, error) {
	if f.listErr != nil {
		return nil, nil, f.listErr
	}
	fn, ok := f.functions[identifier]
	if !ok {
		return nil, nil, api.ErrNotFound
	}
	schedulers := f.schedulers[identifier]
	resp := &apiclient.FunctionSchedulerListResponse{
		Data: append([]apiclient.FunctionScheduler(nil), schedulers...),
	}
	return fn, resp, nil
}

func (f *fakeSchedulers) CreateSchedulerByID(_ context.Context, functionID uuid.UUID, input api.FunctionSchedulerInput) (*apiclient.FunctionScheduler, error) {
	f.createdCalls = append(f.createdCalls, schedulerCreateCall{FunctionID: functionID, Input: input})
	if f.createErr != nil {
		return nil, f.createErr
	}
	schedulerID := uuid.New()
	scheduler := apiclient.FunctionScheduler{
		Id:             &schedulerID,
		FunctionId:     &functionID,
		Name:           &input.Name,
		CronExpression: &input.CronExpression,
		Enabled:        input.Enabled,
	}
	if input.Payload != nil {
		payload := maps.Clone(input.Payload)
		scheduler.Payload = &payload
	}
	if len(input.Regions) > 0 {
		regions := append([]string(nil), input.Regions...)
		scheduler.Regions = &regions
	}
	// Store by function name (need to find it)
	for name, fn := range f.functions {
		if fn.Id == functionID {
			f.schedulers[name] = append(f.schedulers[name], scheduler)
			break
		}
	}
	return &scheduler, nil
}

func (f *fakeSchedulers) UpdateSchedulerByID(_ context.Context, functionID, schedulerID uuid.UUID, input api.FunctionSchedulerInput) (*apiclient.FunctionScheduler, error) {
	f.updatedCalls = append(f.updatedCalls, schedulerUpdateCall{FunctionID: functionID, SchedulerID: schedulerID, Input: input})
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	// Find and update the scheduler
	for name, fn := range f.functions {
		if fn.Id == functionID {
			for i := range f.schedulers[name] {
				if f.schedulers[name][i].Id == nil || *f.schedulers[name][i].Id != schedulerID {
					continue
				}
				f.schedulers[name][i].Name = &input.Name
				f.schedulers[name][i].CronExpression = &input.CronExpression
				f.schedulers[name][i].Enabled = input.Enabled
				if input.Payload != nil {
					payload := maps.Clone(input.Payload)
					f.schedulers[name][i].Payload = &payload
				} else {
					f.schedulers[name][i].Payload = nil
				}
				if len(input.Regions) > 0 {
					regions := append([]string(nil), input.Regions...)
					f.schedulers[name][i].Regions = &regions
				} else {
					f.schedulers[name][i].Regions = nil
				}
				return &f.schedulers[name][i], nil
			}
			break
		}
	}
	return nil, errors.New("scheduler not found")
}

func TestReconcileSchedulersCreatesNew(t *testing.T) {
	functionID := uuid.New()
	schedulers := newFakeSchedulers()
	schedulers.functions["hello"] = &apiclient.Function{
		Id:   functionID,
		Name: "hello",
	}

	svc := NewServiceWithReconcilers(newFakeStorage(), &fakeFunctions{}, schedulers)
	manifest := &Manifest{
		Version: 1,
		Functions: []FunctionManifest{{
			Name: "hello",
			Schedulers: []SchedulerManifest{{
				Name: "daily",
				Cron: "0 0 * * *",
			}},
		}},
	}

	summary, err := svc.Deploy(context.Background(), manifest)
	require.NoError(t, err)
	assert.Equal(t, 1, summary.SchedulersCreated)
	assert.Equal(t, 0, summary.SchedulersUpdated)
	assert.Equal(t, 0, summary.SchedulersUnchanged)

	require.Len(t, schedulers.createdCalls, 1)
	assert.Equal(t, functionID, schedulers.createdCalls[0].FunctionID)
	assert.Equal(t, "daily", schedulers.createdCalls[0].Input.Name)
	assert.Equal(t, "0 0 * * *", schedulers.createdCalls[0].Input.CronExpression)
}

func TestReconcileSchedulersUpdatesChanged(t *testing.T) {
	functionID := uuid.New()
	schedulerID := uuid.New()
	schedulers := newFakeSchedulers()
	schedulers.functions["hello"] = &apiclient.Function{
		Id:   functionID,
		Name: "hello",
	}
	cron := "0 0 * * *"
	enabled := true
	schedulers.schedulers["hello"] = []apiclient.FunctionScheduler{{
		Id:             &schedulerID,
		FunctionId:     &functionID,
		Name:           strPtr("daily"),
		CronExpression: &cron,
		Enabled:        &enabled,
	}}

	svc := NewServiceWithReconcilers(newFakeStorage(), &fakeFunctions{}, schedulers)
	newCron := "0 12 * * *" // Changed time
	manifest := &Manifest{
		Version: 1,
		Functions: []FunctionManifest{{
			Name: "hello",
			Schedulers: []SchedulerManifest{{
				Name: "daily",
				Cron: newCron,
			}},
		}},
	}

	summary, err := svc.Deploy(context.Background(), manifest)
	require.NoError(t, err)
	assert.Equal(t, 0, summary.SchedulersCreated)
	assert.Equal(t, 1, summary.SchedulersUpdated)
	assert.Equal(t, 0, summary.SchedulersUnchanged)

	require.Len(t, schedulers.updatedCalls, 1)
	assert.Equal(t, functionID, schedulers.updatedCalls[0].FunctionID)
	assert.Equal(t, schedulerID, schedulers.updatedCalls[0].SchedulerID)
	assert.Equal(t, "daily", schedulers.updatedCalls[0].Input.Name)
	assert.Equal(t, newCron, schedulers.updatedCalls[0].Input.CronExpression)
}

func TestReconcileSchedulersUnchanged(t *testing.T) {
	functionID := uuid.New()
	schedulerID := uuid.New()
	schedulers := newFakeSchedulers()
	schedulers.functions["hello"] = &apiclient.Function{
		Id:   functionID,
		Name: "hello",
	}
	cron := "0 0 * * *"
	enabled := true
	schedulers.schedulers["hello"] = []apiclient.FunctionScheduler{{
		Id:             &schedulerID,
		FunctionId:     &functionID,
		Name:           strPtr("daily"),
		CronExpression: &cron,
		Enabled:        &enabled,
	}}

	svc := NewServiceWithReconcilers(newFakeStorage(), &fakeFunctions{}, schedulers)
	manifest := &Manifest{
		Version: 1,
		Functions: []FunctionManifest{{
			Name: "hello",
			Schedulers: []SchedulerManifest{{
				Name:    "daily",
				Cron:    "0 0 * * *",
				Enabled: &enabled,
			}},
		}},
	}

	summary, err := svc.Deploy(context.Background(), manifest)
	require.NoError(t, err)
	assert.Equal(t, 0, summary.SchedulersCreated)
	assert.Equal(t, 0, summary.SchedulersUpdated)
	assert.Equal(t, 1, summary.SchedulersUnchanged)

	assert.Empty(t, schedulers.createdCalls)
	assert.Empty(t, schedulers.updatedCalls)
}

func TestReconcileSchedulersDoesNotDeleteUndeclared(t *testing.T) {
	functionID := uuid.New()
	schedulerID := uuid.New()
	schedulers := newFakeSchedulers()
	schedulers.functions["hello"] = &apiclient.Function{
		Id:   functionID,
		Name: "hello",
	}
	cron := "0 0 * * *"
	enabled := true
	// Server has an existing scheduler not in manifest
	schedulers.schedulers["hello"] = []apiclient.FunctionScheduler{{
		Id:             &schedulerID,
		FunctionId:     &functionID,
		Name:           strPtr("adhoc"),
		CronExpression: &cron,
		Enabled:        &enabled,
	}}

	svc := NewServiceWithReconcilers(newFakeStorage(), &fakeFunctions{}, schedulers)
	// Manifest declares a different scheduler
	manifest := &Manifest{
		Version: 1,
		Functions: []FunctionManifest{{
			Name: "hello",
			Schedulers: []SchedulerManifest{{
				Name: "daily",
				Cron: "0 12 * * *",
			}},
		}},
	}

	summary, err := svc.Deploy(context.Background(), manifest)
	require.NoError(t, err)
	assert.Equal(t, 1, summary.SchedulersCreated) // Creates "daily"
	assert.Equal(t, 0, summary.SchedulersUpdated)
	assert.Equal(t, 0, summary.SchedulersUnchanged)

	// Verify "adhoc" was not deleted (still in fake storage)
	assert.Len(t, schedulers.schedulers["hello"], 2, "undeclared scheduler should not be deleted")
}

func TestReconcileSchedulersDuplicateNameError(t *testing.T) {
	functionID := uuid.New()
	schedulerID1 := uuid.New()
	schedulerID2 := uuid.New()
	schedulers := newFakeSchedulers()
	schedulers.functions["hello"] = &apiclient.Function{
		Id:   functionID,
		Name: "hello",
	}
	cron := "0 0 * * *"
	enabled := true
	// Server has duplicate scheduler names
	name := "daily"
	schedulers.schedulers["hello"] = []apiclient.FunctionScheduler{
		{
			Id:             &schedulerID1,
			FunctionId:     &functionID,
			Name:           &name,
			CronExpression: &cron,
			Enabled:        &enabled,
		},
		{
			Id:             &schedulerID2,
			FunctionId:     &functionID,
			Name:           &name,
			CronExpression: &cron,
			Enabled:        &enabled,
		},
	}
	svc := NewServiceWithReconcilers(newFakeStorage(), &fakeFunctions{}, schedulers)
	manifest := &Manifest{
		Version: 1,
		Functions: []FunctionManifest{{
			Name: "hello",
			Schedulers: []SchedulerManifest{{
				Name: "daily",
				Cron: "0 0 * * *",
			}},
		}},
	}

	_, err := svc.Deploy(context.Background(), manifest)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate scheduler name")
	assert.Contains(t, err.Error(), "daily")
}

func strPtr(s string) *string {
	return &s
}

func TestSchedulerNeedsUpdateIdempotency(t *testing.T) {
	cron := "0 9 * * *"
	enabled := true

	base := apiclient.FunctionScheduler{
		CronExpression: &cron,
		Enabled:        &enabled,
	}

	t.Run("omitted regions are server-managed", func(t *testing.T) {
		existing := base
		regions := []string{"us-east-1"}
		existing.Regions = &regions
		desired := SchedulerManifest{Name: "daily", Cron: cron} // regions omitted
		if schedulerNeedsUpdate(existing, desired) {
			t.Fatalf("expected no update when manifest omits regions (server-managed)")
		}
	})

	t.Run("payload int equals server float64", func(t *testing.T) {
		existing := base
		p := map[string]any{"count": float64(5)}
		existing.Payload = &p
		desired := SchedulerManifest{Name: "daily", Cron: cron, Payload: map[string]any{"count": 5}}
		if schedulerNeedsUpdate(existing, desired) {
			t.Fatalf("expected no update for numerically equivalent payload")
		}
	})

	t.Run("omitted payload equals server empty object", func(t *testing.T) {
		existing := base
		empty := map[string]any{}
		existing.Payload = &empty
		desired := SchedulerManifest{Name: "daily", Cron: cron} // payload omitted
		if schedulerNeedsUpdate(existing, desired) {
			t.Fatalf("expected no update when payload omitted")
		}
	})

	t.Run("cron change triggers update", func(t *testing.T) {
		existing := base
		desired := SchedulerManifest{Name: "daily", Cron: "0 10 * * *"}
		if !schedulerNeedsUpdate(existing, desired) {
			t.Fatalf("expected update when cron differs")
		}
	})

	t.Run("explicit region mismatch triggers update", func(t *testing.T) {
		existing := base
		regions := []string{"us-east-1"}
		existing.Regions = &regions
		desired := SchedulerManifest{Name: "daily", Cron: cron, Regions: []string{"eu-west-1"}}
		if !schedulerNeedsUpdate(existing, desired) {
			t.Fatalf("expected update when explicit regions differ")
		}
	})

	t.Run("nil existing enabled treated as enabled", func(t *testing.T) {
		existing := apiclient.FunctionScheduler{CronExpression: &cron} // Enabled nil
		desired := SchedulerManifest{Name: "daily", Cron: cron}        // enabled omitted -> true
		if schedulerNeedsUpdate(existing, desired) {
			t.Fatalf("expected no update: nil existing Enabled should be treated as enabled")
		}
	})

	t.Run("omitted payload is server-managed (no churn)", func(t *testing.T) {
		existing := base
		serverPayload := map[string]any{"job": "refresh"}
		existing.Payload = &serverPayload
		desired := SchedulerManifest{Name: "daily", Cron: cron} // payload omitted
		// Manifest omits payload; the API can't clear it and the server keeps its
		// value, so reporting an update would never converge. Expect no update.
		if schedulerNeedsUpdate(existing, desired) {
			t.Fatalf("expected no update when manifest omits payload (server-managed)")
		}
	})
}

func TestReconcileSchedulersReenablesAndConverges(t *testing.T) {
	functionID := uuid.New()
	schedulerID := uuid.New()
	schedulers := newFakeSchedulers()
	schedulers.functions["hello"] = &apiclient.Function{Id: functionID, Name: "hello"}
	cron := "0 0 * * *"
	disabled := false
	// Server scheduler is disabled; the manifest omits enabled (defaults to true).
	schedulers.schedulers["hello"] = []apiclient.FunctionScheduler{{
		Id:             &schedulerID,
		FunctionId:     &functionID,
		Name:           strPtr("daily"),
		CronExpression: &cron,
		Enabled:        &disabled,
	}}

	svc := NewServiceWithReconcilers(newFakeStorage(), &fakeFunctions{}, schedulers)
	manifest := &Manifest{
		Version: 1,
		Functions: []FunctionManifest{{
			Name:       "hello",
			Schedulers: []SchedulerManifest{{Name: "daily", Cron: cron}}, // enabled omitted
		}},
	}

	// First deploy: re-enables the scheduler with an explicit Enabled=true.
	summary, err := svc.Deploy(context.Background(), manifest)
	require.NoError(t, err)
	assert.Equal(t, 1, summary.SchedulersUpdated)
	require.Len(t, schedulers.updatedCalls, 1)
	require.NotNil(t, schedulers.updatedCalls[0].Input.Enabled)
	assert.True(t, *schedulers.updatedCalls[0].Input.Enabled, "omitted enabled must be sent as true so the update applies")

	// Second deploy: scheduler is now enabled, so the run converges (no churn).
	summary, err = svc.Deploy(context.Background(), manifest)
	require.NoError(t, err)
	assert.Equal(t, 0, summary.SchedulersUpdated)
	assert.Equal(t, 1, summary.SchedulersUnchanged)
	assert.Len(t, schedulers.updatedCalls, 1, "second deploy must not issue another update")
}
