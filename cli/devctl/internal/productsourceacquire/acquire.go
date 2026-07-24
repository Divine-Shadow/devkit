package productsourceacquire

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"time"
)

var packageManifestPath string

type typedError struct {
	stage string
	code  string
}

func (err typedError) Error() string { return err.stage + "/" + err.code }

func Execute(ctx context.Context, args []string, output io.Writer) int {
	return executeWithBoundary(ctx, args, packageManifestPath, output, productionBoundary{})
}

func executeWithBoundary(
	ctx context.Context,
	args []string,
	manifestPath string,
	output io.Writer,
	boundary operationBoundary,
) int {
	receipt := newReceipt(manifestPath)
	if len(args) != 0 {
		receipt.Failure = &Failure{Stage: "invocation", Code: "arguments_forbidden"}
		receipt.Teardown.Action = "no-lifecycle-root-created"
		emitReceipt(output, &receipt)
		return 64
	}
	manifest, digest, err := loadManifest(manifestPath)
	if err != nil {
		receipt.Failure = &Failure{Stage: "manifest", Code: "invalid_package_binding"}
		receipt.Teardown.Action = "no-lifecycle-root-created"
		emitReceipt(output, &receipt)
		return 65
	}
	bindReceipt(&receipt, manifest, digest)
	operationCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	err = acquire(operationCtx, manifest, boundary, &receipt)
	if err != nil {
		var typed typedError
		if errors.As(err, &typed) {
			receipt.Failure = &Failure{Stage: typed.stage, Code: typed.code}
		} else {
			receipt.Failure = &Failure{Stage: "acquisition", Code: "unclassified_failure"}
		}
		receipt.Status = "failure"
		if receipt.RootOwnershipAcquired {
			receipt.Tainted = true
			receipt.TaintMarkerWritten = createExclusive(
				manifest.Product.TaintMarkerPath,
				[]byte("devkit/product-source-acquisition-tainted/v1\n"),
			) == nil
			writeReceiptFile(manifest.Product.ReceiptPath, &receipt)
		}
		emitReceipt(output, &receipt)
		return 1
	}
	receipt.Status = "success"
	writeReceiptFile(manifest.Product.ReceiptPath, &receipt)
	if !receipt.ReceiptWritten {
		receipt.Status = "failure"
		receipt.Failure = &Failure{Stage: "receipt", Code: "create_failed"}
		receipt.Tainted = true
		receipt.TaintMarkerWritten = createExclusive(
			manifest.Product.TaintMarkerPath,
			[]byte("devkit/product-source-acquisition-tainted/v1\n"),
		) == nil
		emitReceipt(output, &receipt)
		return 1
	}
	emitReceipt(output, &receipt)
	return 0
}

func bindReceipt(receipt *Receipt, manifest Manifest, digest string) {
	receipt.ManifestSHA256 = digest
	receipt.ProductOrigin = manifest.Product.Origin
	receipt.ProductRevision = manifest.Product.Revision
	receipt.LifecycleRoot = manifest.Product.LifecycleRoot
	receipt.CheckoutPath = manifest.Product.CheckoutPath
	receipt.ReceiptPath = manifest.Product.ReceiptPath
}

func acquire(
	ctx context.Context,
	manifest Manifest,
	boundary operationBoundary,
	receipt *Receipt,
) error {
	if !checkoutRelativeToRoot(manifest.Product.LifecycleRoot, manifest.Product.CheckoutPath) {
		return typedError{stage: "manifest", code: "invalid_checkout_geometry"}
	}
	if err := os.Mkdir(manifest.Product.LifecycleRoot, 0o700); err != nil {
		if errors.Is(err, os.ErrExist) {
			receipt.Teardown.Action = "refuse-existing-lifecycle-root"
			return typedError{stage: "precondition", code: "lifecycle_root_not_absent"}
		}
		return typedError{stage: "lifecycle", code: "root_create_failed"}
	}
	receipt.RootOwnershipAcquired = true
	if err := ensureRootMode(manifest.Product.LifecycleRoot); err != nil {
		return typedError{stage: "lifecycle", code: "root_mode_invalid"}
	}

	transport, err := boundary.StartTransport(ctx, manifest)
	transportStopped := false
	if transport != nil {
		defer func() {
			if !transportStopped {
				stopped := transport.Stop()
				receipt.OwnedRuntimeStop.TransportProcessStopped = stopped.processStopped
				receipt.OwnedRuntimeStop.TransportSocketStopped = stopped.socketStopped
			}
		}()
	}
	if err != nil || transport == nil {
		return typedError{stage: "transport", code: "start_failed"}
	}

	if _, err := boundary.RunGit(ctx, manifest,
		"clone", "--no-checkout", "--origin", "origin", "--",
		manifest.Product.Origin, manifest.Product.CheckoutPath,
	); err != nil {
		return typedError{stage: "git_clone", code: "command_failed"}
	}
	result, err := boundary.RunGit(ctx, manifest,
		"-C", manifest.Product.CheckoutPath, "remote", "get-url", "origin",
	)
	if err != nil || result.stdout != manifest.Product.Origin {
		return typedError{stage: "origin_verification", code: "mismatch"}
	}
	receipt.OriginVerified = true
	if _, err := boundary.RunGit(ctx, manifest,
		"-C", manifest.Product.CheckoutPath, "fetch", "--force", "--no-tags", "origin", manifest.Product.Revision,
	); err != nil {
		return typedError{stage: "git_fetch", code: "command_failed"}
	}
	if _, err := boundary.RunGit(ctx, manifest,
		"-C", manifest.Product.CheckoutPath, "checkout", "--detach", manifest.Product.Revision,
	); err != nil {
		return typedError{stage: "git_checkout", code: "command_failed"}
	}
	result, err = boundary.RunGit(ctx, manifest,
		"-C", manifest.Product.CheckoutPath, "rev-parse", "HEAD",
	)
	if err != nil || result.stdout != manifest.Product.Revision {
		return typedError{stage: "head_verification", code: "mismatch"}
	}
	receipt.HeadVerified = true
	result, err = boundary.RunGit(ctx, manifest,
		"-C", manifest.Product.CheckoutPath, "status", "--porcelain=v1", "--untracked-files=all",
	)
	if err != nil || result.stdout != "" {
		return typedError{stage: "clean_worktree_verification", code: "not_clean"}
	}
	receipt.CleanWorktreeVerified = true
	result, err = boundary.RunGit(ctx, manifest,
		"-C", manifest.Product.CheckoutPath, "rev-parse", "HEAD^{tree}",
	)
	if err != nil || !revisionPattern.MatchString(result.stdout) {
		return typedError{stage: "content_identity", code: "invalid_tree"}
	}
	tree := result.stdout
	const verificationRef = "refs/devkit/source-acquisition-verification"
	if _, err := boundary.RunGit(ctx, manifest,
		"-C", manifest.Product.CheckoutPath, "update-ref", verificationRef,
		manifest.Product.Revision, strings.Repeat("0", 40),
	); err != nil {
		return typedError{stage: "git_metadata", code: "write_failed"}
	}
	result, err = boundary.RunGit(ctx, manifest,
		"-C", manifest.Product.CheckoutPath, "rev-parse", verificationRef,
	)
	if err != nil || result.stdout != manifest.Product.Revision {
		return typedError{stage: "git_metadata", code: "readback_mismatch"}
	}
	if _, err := boundary.RunGit(ctx, manifest,
		"-C", manifest.Product.CheckoutPath, "update-ref", "-d", verificationRef, manifest.Product.Revision,
	); err != nil {
		return typedError{stage: "git_metadata", code: "owned_ref_release_failed"}
	}
	receipt.GitMetadataWriteProved = true
	result, err = boundary.RunGit(ctx, manifest,
		"-C", manifest.Product.CheckoutPath, "status", "--porcelain=v1", "--untracked-files=all",
	)
	if err != nil || result.stdout != "" {
		return typedError{stage: "final_clean_worktree_verification", code: "not_clean"}
	}
	receipt.ContentIdentity = &ContentIdentity{Kind: "git-commit-tree", Revision: manifest.Product.Revision, Tree: tree}
	receipt.LocalGitFlakeInput = &LocalGitFlakeInput{
		URL:      localGitFlakeURL(manifest.Product.CheckoutPath, manifest.Product.Revision),
		Revision: manifest.Product.Revision,
		Tree:     tree,
	}

	stopped := transport.Stop()
	transportStopped = true
	receipt.OwnedRuntimeStop.TransportProcessStopped = stopped.processStopped
	receipt.OwnedRuntimeStop.TransportSocketStopped = stopped.socketStopped
	if !stopped.processStopped || !stopped.socketStopped {
		return typedError{stage: "transport", code: "stop_incomplete"}
	}
	return nil
}

func writeReceiptFile(path string, receipt *Receipt) {
	receipt.completeClock()
	receipt.ReceiptWritten = true
	data, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		receipt.ReceiptWritten = false
		return
	}
	data = append(data, '\n')
	if err := createExclusive(path, data); err != nil {
		receipt.ReceiptWritten = false
	}
}

func emitReceipt(output io.Writer, receipt *Receipt) {
	receipt.completeClock()
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(receipt)
}
