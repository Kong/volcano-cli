package functions

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/archive"
	clifunction "github.com/Kong/volcano-cli/internal/function"
	"github.com/Kong/volcano-cli/internal/output"
	"github.com/Kong/volcano-cli/internal/projectconfig"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const deployBatchSize = 100

type deployOptions struct {
	deps     cliruntime.Deps
	file     string
	all      bool
	batchAll bool
	out      io.Writer
}

func newDeploy(deps cliruntime.Deps, batchAll bool) *cobra.Command {
	var file string
	var all bool
	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Deploy functions",
		Long: fmt.Sprintf(`Deploy functions from the volcano/functions directory.

Usage:
  %s
  %s
  %s
  %s

The CLI scans volcano/functions, detects the runtime from source file extensions,
packages source with dependency manifests and shared libraries, and uploads the
archive to Volcano. Cloud deploy-all uploads are split into batches of up to
100 functions; local deploy-all uploads each function individually.`,
			cliruntime.CommandPath(deps, "functions deploy --all"),
			cliruntime.CommandPath(deps, "functions deploy -a"),
			cliruntime.CommandPath(deps, "functions deploy -f get-notes"),
			cliruntime.CommandPath(deps, "functions deploy -f volcano/functions/get-notes.js")),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDeploy(cmd.Context(), deployOptions{
				deps:     deps,
				file:     strings.TrimSpace(file),
				all:      all,
				batchAll: batchAll,
				out:      cmd.OutOrStdout(),
			})
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", "", "Deploy a specific function by name or path")
	cmd.Flags().BoolVarP(&all, "all", "a", false, "Deploy all functions")
	return cmd
}

func runDeploy(ctx context.Context, opts deployOptions) error {
	if opts.all && opts.file != "" {
		return errors.New("cannot use --all and --file together")
	}
	if !opts.all && opts.file == "" {
		return errors.New("specify either --all to deploy all functions or --file/-f to deploy a specific function")
	}

	service := clifunction.NewService(opts.deps)
	runtimeCatalog, err := service.RuntimeCatalog(ctx)
	if err != nil {
		return err
	}
	baseDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	fmt.Fprintln(opts.out, "\nScanning volcano/functions/...")
	allSources, err := clifunction.ScanSources(baseDir, runtimeCatalog)
	if err != nil {
		return fmt.Errorf("failed to scan functions: %w", err)
	}
	if len(allSources) == 0 {
		if !opts.all {
			return fmt.Errorf("function %q not found in volcano/functions/", opts.file)
		}
		fmt.Fprintln(opts.out, "No functions found in volcano/functions/")
		fmt.Fprintln(opts.out, "\nMake sure your functions are in the volcano/functions/ directory")
		return nil
	}

	sources := allSources
	if !opts.all {
		sources = nil
		for _, source := range allSources {
			if sourceMatchesTarget(source, opts.file, baseDir) {
				sources = append(sources, source)
				break
			}
		}
		if len(sources) == 0 {
			return fmt.Errorf("function %q not found in volcano/functions/\navailable functions: %s", opts.file, formatSourceNames(allSources))
		}
		fmt.Fprintf(opts.out, "Deploying function: %s\n", sources[0].Name)
	} else {
		fmt.Fprintf(opts.out, "Found %d function(s)\n", len(sources))
	}

	declarations := projectconfig.FunctionVariableDeclarations("")

	if opts.all {
		return runDeployAll(ctx, opts.out, service, baseDir, sources, opts.batchAll, declarations)
	}
	return runDeployOne(ctx, opts.out, service, baseDir, sources[0], declarations)
}

// applyVariableDeclaration attaches the manifest declaration for pkg, if the
// manifest declared one. Without a matching entry both fields stay nil and
// deploy sends neither, leaving the function's stored scope unchanged.
func applyVariableDeclaration(pkg *clifunction.Package, declarations map[string]projectconfig.FunctionVariableDeclaration) {
	declaration, ok := declarations[pkg.Name]
	if !ok {
		return
	}
	pkg.VariableScope = declaration.VariableScope
	pkg.Variables = declaration.Variables
}

func runDeployOne(ctx context.Context, out io.Writer, service clifunction.Service, baseDir string, source clifunction.SourceInfo, declarations map[string]projectconfig.FunctionVariableDeclaration) error {
	fmt.Fprintf(out, "\n[1/1] Deploying %s...\n", source.Name)
	printSourceSummary(out, source)
	pkg, err := clifunction.PackageSource(source, baseDir)
	if err != nil {
		return fmt.Errorf("failed to package function %s: %w", source.Name, err)
	}
	fmt.Fprintf(out, "  Archive size: %s\n", archive.FormatSize(pkg.Size))
	applyVariableDeclaration(pkg, declarations)

	deployed, err := service.DeployPackage(ctx, *pkg)
	if err != nil {
		return err
	}
	output.Success(out, "Function '%s' deployment started (%s): %s", deployed.Name, deployRuntimeValue(deployed.Runtime), archive.FormatSize(pkg.Size))
	fmt.Fprintf(out, "  Deployed %s\n", deployed.Name)
	fmt.Fprintln(out)
	output.Success(out, "1/1 functions deployment started")
	return nil
}

func runDeployAll(ctx context.Context, out io.Writer, service clifunction.Service, baseDir string, sources []clifunction.SourceInfo, batch bool, declarations map[string]projectconfig.FunctionVariableDeclaration) error {
	packages := make([]clifunction.Package, 0, len(sources))
	var totalSize int64
	for i, source := range sources {
		fmt.Fprintf(out, "\n[%d/%d] Packaging %s...\n", i+1, len(sources), source.Name)
		printSourceSummary(out, source)
		pkg, err := clifunction.PackageSource(source, baseDir)
		if err != nil {
			return fmt.Errorf("failed to package function %s: %w", source.Name, err)
		}
		fmt.Fprintf(out, "  Archive size: %s\n", archive.FormatSize(pkg.Size))
		applyVariableDeclaration(pkg, declarations)
		totalSize += pkg.Size
		packages = append(packages, *pkg)
	}

	fmt.Fprintf(out, "\nTotal upload size: %s\n", archive.FormatSize(totalSize))
	if !batch {
		return runDeployAllIndividually(ctx, out, service, packages)
	}

	totalStarted := 0
	totalFailed := 0
	batchCount := 0
	for start := 0; start < len(packages); start += deployBatchSize {
		end := min(start+deployBatchSize, len(packages))
		fmt.Fprintf(out, "\nUploading batch %d-%d of %d...\n", start+1, end, len(packages))
		resp, err := service.DeployPackageBatch(ctx, packages[start:end])
		if err != nil {
			return fmt.Errorf("failed to deploy functions batch %d-%d: %w", start+1, end, err)
		}
		batchCount++
		for _, fn := range resp.Data {
			fmt.Fprintf(out, "  Deployed %s\n", fn.Name)
		}
		failures := batchFailures(resp)
		for _, failure := range failures {
			fmt.Fprintf(out, "  ✗ Failed %s: %s\n", failure.Name, failure.Error)
		}
		totalStarted += len(resp.Data)
		totalFailed += len(failures)
	}

	if totalStarted == 0 && totalFailed > 0 {
		return fmt.Errorf("0/%d functions deployment started", len(sources))
	}
	if totalFailed > 0 {
		message := fmt.Sprintf("%d/%d functions deployment started across %d batch(es); %d failed before workflow start", totalStarted, len(sources), batchCount, totalFailed)
		fmt.Fprintf(out, "Warning: %s\n", message)
		return errors.New(message)
	}
	output.Success(out, "%d/%d functions deployment started across %d batch(es)", totalStarted, len(sources), batchCount)
	return nil
}

func runDeployAllIndividually(ctx context.Context, out io.Writer, service clifunction.Service, packages []clifunction.Package) error {
	totalStarted := 0
	for i, pkg := range packages {
		fmt.Fprintf(out, "\nUploading %s (%d/%d)...\n", pkg.Name, i+1, len(packages))
		deployed, err := service.DeployPackage(ctx, pkg)
		if err != nil {
			return fmt.Errorf("failed to deploy function %s: %w", pkg.Name, err)
		}
		fmt.Fprintf(out, "  Deployed %s\n", deployed.Name)
		totalStarted++
	}

	fmt.Fprintln(out)
	output.Success(out, "%d/%d functions deployment started", totalStarted, len(packages))
	return nil
}

func printSourceSummary(out io.Writer, source clifunction.SourceInfo) {
	detectedFrom := filepath.Ext(source.Path)
	if source.IsDir {
		detectedFrom = filepath.Base(source.Path)
	}
	fmt.Fprintf(out, "  Runtime: %s (detected from %s)\n", source.Runtime.Name, detectedFrom)
	fmt.Fprintf(out, "  Function code: %s\n", source.Path)
}

func normalizeSourceTarget(target string) string {
	name := filepath.Base(strings.TrimSpace(target))
	if ext := filepath.Ext(name); ext != "" {
		name = strings.TrimSuffix(name, ext)
	}
	return name
}

func sourceMatchesTarget(source clifunction.SourceInfo, target, baseDir string) bool {
	target = strings.TrimSpace(target)
	if source.Name == normalizeSourceTarget(target) {
		return true
	}

	sourcePath, err := filepath.Abs(source.Path)
	if err != nil {
		return false
	}
	targetPath := target
	if !filepath.IsAbs(targetPath) {
		targetPath = filepath.Join(baseDir, targetPath)
	}
	targetPath, err = filepath.Abs(targetPath)
	if err != nil {
		return false
	}
	return filepath.Clean(sourcePath) == filepath.Clean(targetPath)
}

func formatSourceNames(sources []clifunction.SourceInfo) string {
	names := make([]string, len(sources))
	for i, source := range sources {
		names[i] = source.Name
	}
	return strings.Join(names, ", ")
}

func batchFailures(resp *apiclient.BatchFunctionDeployResponse) []apiclient.BatchFunctionDeployFailure {
	if resp == nil || resp.Failed == nil {
		return nil
	}
	return *resp.Failed
}

func deployRuntimeValue(value *string) string {
	if value == nil {
		return "-"
	}
	if runtime := strings.TrimSpace(*value); runtime != "" {
		return runtime
	}
	return "-"
}
