package variable

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// LambdaReservedNamesURL documents environment variable names reserved by AWS Lambda.
const LambdaReservedNamesURL = "https://docs.aws.amazon.com/lambda/latest/dg/configuration-envvars.html"

var reservedNames = []string{
	"_HANDLER",
	"_X_AMZN_TRACE_ID",
	"AWS_DEFAULT_REGION",
	"AWS_REGION",
	"AWS_EXECUTION_ENV",
	"AWS_LAMBDA_FUNCTION_NAME",
	"AWS_LAMBDA_FUNCTION_MEMORY_SIZE",
	"AWS_LAMBDA_FUNCTION_VERSION",
	"AWS_LAMBDA_INITIALIZATION_TYPE",
	"AWS_LAMBDA_LOG_GROUP_NAME",
	"AWS_LAMBDA_LOG_STREAM_NAME",
	"AWS_ACCESS_KEY",
	"AWS_ACCESS_KEY_ID",
	"AWS_SECRET_ACCESS_KEY",
	"AWS_SESSION_TOKEN",
	"AWS_LAMBDA_RUNTIME_API",
	"LAMBDA_TASK_ROOT",
	"LAMBDA_RUNTIME_DIR",
	"AWS_LAMBDA_MAX_CONCURRENCY",
	"AWS_LAMBDA_METADATA_API",
	"AWS_LAMBDA_METADATA_TOKEN",
}

// ReservedNames returns the environment variable names reserved by AWS Lambda.
func ReservedNames() []string {
	return slices.Clone(reservedNames)
}

// ValidateNames rejects environment variable names reserved by AWS Lambda.
func ValidateNames(names []string) error {
	reserved := make(map[string]struct{}, len(reservedNames))
	for _, name := range reservedNames {
		reserved[name] = struct{}{}
	}

	var found []string
	for _, name := range names {
		if _, ok := reserved[name]; ok {
			found = append(found, name)
		}
	}
	if len(found) == 0 {
		return nil
	}

	sort.Strings(found)
	return fmt.Errorf("reserved variable names cannot be deployed: %s; Volcano Functions use AWS Lambda; see %s", strings.Join(found, ", "), LambdaReservedNamesURL)
}
